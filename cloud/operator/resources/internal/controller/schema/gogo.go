package schema

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	kubernetes "k8s.io/client-go/kubernetes"
	scheme "k8s.io/client-go/kubernetes/scheme"
	rest "k8s.io/client-go/rest"
	remotecommand "k8s.io/client-go/tools/remotecommand"
)

// upgrade:check takes several seconds and prints nothing when no upgrade is
// pending. The marker that the following echo prints tells that it finished,
// and the session stays open until then, for at most two minutes.
const upgradeCheckMarker = "upgrade-check-done"

const upgradeCheckScript = `
output=$(mktemp)

(
	printf 'upgrade:check\necho ` + upgradeCheckMarker + `\n'

	attempt=0

	while [ ${attempt} -lt 120 ] && ! grep --quiet '^g! ` + upgradeCheckMarker + `' "${output}"
	do
		sleep 1

		attempt=$((attempt + 1))
	done
) | telnet localhost %d > "${output}" 2>&1

cat "${output}"

rm -f "${output}"
`

func (gogoUpgradeChecker *GogoUpgradeChecker) GetPendingUpgrades(
	container string,
	context context.Context,
	pod *corev1.Pod,
) (string, error) {
	port := gogoUpgradeChecker.Port

	if port == 0 {
		port = 11311
	}

	request := gogoUpgradeChecker.Clientset.CoreV1().RESTClient().Post().Resource(
		"pods",
	).Namespace(
		pod.Namespace,
	).Name(
		pod.Name,
	).SubResource(
		"exec",
	).VersionedParams(
		&corev1.PodExecOptions{
			Command:   []string{"sh", "-c", fmt.Sprintf(upgradeCheckScript, port)},
			Container: container,
			Stderr:    true,
			Stdout:    true,
		},
		scheme.ParameterCodec,
	)

	executor, error := remotecommand.NewSPDYExecutor(
		gogoUpgradeChecker.Config, "POST", request.URL(),
	)

	if error != nil {
		return "", error
	}

	var stdout bytes.Buffer

	if error := executor.StreamWithContext(
		context,
		remotecommand.StreamOptions{Stderr: &stdout, Stdout: &stdout},
	); error != nil {
		return "", fmt.Errorf("unable to run \"upgrade:check\" in pod %s: %w", pod.Name, error)
	}

	return parseUpgradeCheck(stdout.String())
}

// parseUpgradeCheck returns what upgrade:check printed between the Gogo
// banner and the marker, without prompts.
func parseUpgradeCheck(output string) (string, error) {
	_, afterBanner, found := strings.Cut(output, "Welcome to Apache Felix Gogo")

	if !found {
		return "", fmt.Errorf("the Gogo shell did not answer: %q", strings.TrimSpace(output))
	}

	checkOutput, _, found := strings.Cut(afterBanner, "g! "+upgradeCheckMarker)

	if !found {
		return "", fmt.Errorf("\"upgrade:check\" did not finish: %q", strings.TrimSpace(afterBanner))
	}

	lines := []string{}

	for _, line := range strings.Split(checkOutput, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "g!"))

		if line != "" {
			lines = append(lines, line)
		}
	}

	return strings.Join(lines, "; "), nil
}

// GogoUpgradeChecker runs the Gogo shell command upgrade:check in the DXP
// container. It lists the modules whose registered upgrade steps go beyond
// the schema that Release_ records, which Release_ alone cannot tell.
type GogoUpgradeChecker struct {
	Clientset kubernetes.Interface
	Config    *rest.Config
	Port      int
}
