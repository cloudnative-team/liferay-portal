package schema

import (
	"fmt"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ptr "k8s.io/utils/ptr"
)

const (
	DatabaseJobPostcheck = "postcheck"
	DatabaseJobPrecheck  = "precheck"
	DatabaseJobProbe     = "probe"
	DatabaseJobReset     = "reset"
	DatabaseJobVerify    = "verify"

	labelDatabaseJob = "dxp.liferay.com/schema-database-job"
	labelStatefulSet = "dxp.liferay.com/schema-statefulset"
)

// The DXP container reads its JDBC settings from LIFERAY_JDBC_PERIOD_DEFAULT_*
// variables, whose values may point at other variables with ${env.NAME}.
// With IAM database authentication the proxy ignores the password.
const databaseScriptPrefix = `
set -o errexit

resolve() {
	case "${1}" in
		'${env.'*'}')
			name="${1#\${env.}"
			name="${name%\}}"

			eval "printf '%s' \"\${${name}}\""
			;;
		*)
			printf '%s' "${1}"
			;;
	esac
}

url="${LIFERAY_JDBC_PERIOD_DEFAULT_PERIOD_URL:?The JDBC URL is not set}"

case "${url}" in
	jdbc:postgresql://*) ;;
	*) echo "Only PostgreSQL is supported: ${url}" | tee /dev/termination-log; exit 1 ;;
esac

address="${url#jdbc:postgresql://}"
address="${address%%\?*}"

export PGDATABASE="${address#*/}"
export PGHOST="${address%%/*}"
export PGPORT=5432

case "${PGHOST}" in
	*:*) PGPORT="${PGHOST##*:}"; PGHOST="${PGHOST%%:*}" ;;
esac

export PGCONNECT_TIMEOUT=10
export PGPASSWORD="$(resolve "${LIFERAY_JDBC_PERIOD_DEFAULT_PERIOD_PASSWORD:-iam}")"
export PGUSER="$(resolve "${LIFERAY_JDBC_PERIOD_DEFAULT_PERIOD_USERNAME:?The JDBC user is not set}")"

for attempt in 1 2 3 4 5 6 7 8 9 10 11 12
do
	if psql --command "SELECT 1" > /dev/null 2>&1
	then
		break
	fi

	sleep 5
done
`

// The probe reports the number of tables and what Release_ says about the
// schema: the portal row's schemaVersion and the modules whose state_ is not
// 0 (a failed upgrade or verify). Release_ is written by DXP itself, so it
// tells what the database is at, not what the operator remembers.
const probeScript = `
tables=$(psql --command "SELECT count(*) FROM pg_tables WHERE schemaname = current_schema()" --no-align --tuples-only)

failed=0
failedModules=""
portal=""

if [ "$(psql --command "SELECT to_regclass('release_') IS NOT NULL" --no-align --tuples-only)" = "t" ]
then
	failed=$(psql --command "SELECT count(*) FROM release_ WHERE state_ <> 0" --no-align --tuples-only)
	failedModules=$(psql --command "SELECT string_agg(servletcontextname, ',' ORDER BY servletcontextname) FROM (SELECT servletcontextname FROM release_ WHERE state_ <> 0 LIMIT 10) failed_releases" --no-align --tuples-only)
	portal=$(psql --command "SELECT schemaversion FROM release_ WHERE servletcontextname = 'portal'" --no-align --tuples-only)
fi

printf 'tables=%s\nportal=%s\nfailed=%s\nfailedModules=%s\n' "${tables}" "${portal}" "${failed}" "${failedModules}" | tee /dev/termination-log
`

// The reset drops what the DXP user created and keeps the grants that the
// platform gave it, which DROP OWNED would revoke. Kubernetes turns $$ in a
// command into $, so the DO block uses a named dollar quote.
const resetScript = `
psql --set ON_ERROR_STOP=1 <<'EOF'
DO $reset$
DECLARE
	r record;
BEGIN
	FOR r IN SELECT viewname FROM pg_views WHERE schemaname = current_schema() AND viewowner = current_user LOOP
		EXECUTE format('DROP VIEW IF EXISTS %I CASCADE', r.viewname);
	END LOOP;

	FOR r IN SELECT tablename FROM pg_tables WHERE schemaname = current_schema() AND tableowner = current_user LOOP
		EXECUTE format('DROP TABLE IF EXISTS %I CASCADE', r.tablename);
	END LOOP;

	FOR r IN SELECT sequencename FROM pg_sequences WHERE schemaname = current_schema() AND sequenceowner = current_user LOOP
		EXECUTE format('DROP SEQUENCE IF EXISTS %I CASCADE', r.sequencename);
	END LOOP;
END $reset$;
EOF

tables=$(psql --command "SELECT count(*) FROM pg_tables WHERE schemaname = current_schema()" --no-align --tuples-only)

echo "tables=${tables}" | tee /dev/termination-log
`

// newDatabaseJob builds a Job that reaches the database the way the DXP pods
// do: the pod template's service account, its sidecars (for example the
// Cloud SQL Auth Proxy) and the DXP container's JDBC environment. A psql
// container then runs the probe or the reset. Only PostgreSQL is supported.
func newDatabaseJob(
	attempt int,
	databaseClientImage string,
	kind string,
	statefulSet *appsv1.StatefulSet,
) *batchv1.Job {
	podSpec := statefulSet.Spec.Template.Spec

	sidecars := []corev1.Container{}

	for _, initContainer := range podSpec.InitContainers {
		if (initContainer.RestartPolicy != nil) &&
			(*initContainer.RestartPolicy == corev1.ContainerRestartPolicyAlways) {

			sidecars = append(sidecars, initContainer)
		}
	}

	var dxpContainer corev1.Container

	for _, container := range podSpec.Containers {
		if container.Name == statefulSet.Name {
			dxpContainer = container
		}
	}

	if (dxpContainer.Name == "") && (len(podSpec.Containers) > 0) {
		dxpContainer = podSpec.Containers[0]
	}

	script := probeScript

	if kind == DatabaseJobReset {
		script = resetScript
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				labelDatabaseJob: kind,
				labelStatefulSet: statefulSet.Name,
			},
			Name:      fmt.Sprintf("%s-schema-%s-%d", statefulSet.Name, kind, attempt),
			Namespace: statefulSet.Namespace,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: ptr.To(int32(2)),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{labelDatabaseJob: kind},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Command: []string{"sh", "-c", databaseScriptPrefix + script},
							Env: append(
								dxpContainer.Env,
								corev1.EnvVar{Name: "HOME", Value: "/tmp"},
							),
							EnvFrom: dxpContainer.EnvFrom,
							Image:   databaseClientImage,
							Name:    "psql",
							SecurityContext: &corev1.SecurityContext{
								AllowPrivilegeEscalation: ptr.To(false),
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{"ALL"},
								},
								RunAsNonRoot: ptr.To(true),
								RunAsUser:    ptr.To(int64(1000)),
								SeccompProfile: &corev1.SeccompProfile{
									Type: corev1.SeccompProfileTypeRuntimeDefault,
								},
							},
							TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
						},
					},
					ImagePullSecrets:   podSpec.ImagePullSecrets,
					InitContainers:     sidecars,
					RestartPolicy:      corev1.RestartPolicyNever,
					SecurityContext:    podSpec.SecurityContext,
					ServiceAccountName: podSpec.ServiceAccountName,
					Volumes:            podSpec.Volumes,
				},
			},
		},
	}
}

func parseDatabaseState(message string) (databaseState, error) {
	state := databaseState{}

	found := false

	for _, line := range strings.Split(message, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")

		if !ok {
			continue
		}

		var error error

		switch key {
		case "failed":
			state.failed, error = strconv.Atoi(value)
		case "failedModules":
			state.failedModules = value
		case "portal":
			state.portal = value
		case "tables":
			found = true
			state.tables, error = strconv.Atoi(value)
		}

		if error != nil {
			return state, fmt.Errorf("invalid %s in %q", key, message)
		}
	}

	if !found {
		return state, fmt.Errorf("the database job did not report a table count in %q", message)
	}

	return state, nil
}

// databaseState is what a database Job reported.
type databaseState struct {
	failed        int
	failedModules string
	portal        string
	tables        int
}
