package main

import (
	"os"
	"time"

	env "github.com/caarlos0/env/v11"
	cxv1alpha1 "github.com/liferay/liferay-portal/cloud/operator/api/cx/v1alpha1"
	licensingv1alpha1 "github.com/liferay/liferay-portal/cloud/operator/api/licensing/v1alpha1"
	addon "github.com/liferay/liferay-portal/cloud/operator/internal/addon"
	controller "github.com/liferay/liferay-portal/cloud/operator/internal/controller"
	cx "github.com/liferay/liferay-portal/cloud/operator/internal/controller/cx"
	licensing "github.com/liferay/liferay-portal/cloud/operator/internal/controller/licensing"
	schema "github.com/liferay/liferay-portal/cloud/operator/internal/controller/schema"
	provisioning "github.com/liferay/liferay-portal/cloud/operator/internal/provisioning"
	corev1 "k8s.io/api/core/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	runtime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	kubernetes "k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	controllerruntime "sigs.k8s.io/controller-runtime"
	cache "sigs.k8s.io/controller-runtime/pkg/cache"
	client "sigs.k8s.io/controller-runtime/pkg/client"
	healthz "sigs.k8s.io/controller-runtime/pkg/healthz"
	zap "sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(cxv1alpha1.AddToScheme(scheme))
	utilruntime.Must(licensingv1alpha1.AddToScheme(scheme))
}

func main() {
	config, configError := env.ParseAs[config]()

	controllerruntime.SetLogger(zap.New(zap.UseDevMode(config.Debug)))

	if configError != nil {
		controller.SetupLog.Error(configError, "Unable to read configuration, falling back to defaults")
	}

	cxConfigMapSelector, error := labels.Parse(cx.LabelMetadataType)

	if error != nil {
		controller.SetupLog.Error(error, "Unable to build the ConfigMap cache selector")

		os.Exit(1)
	}

	byObject := map[client.Object]cache.ByObject{
		&corev1.ConfigMap{}: {Label: cxConfigMapSelector},
	}

	if config.SchemaGateEnabled {

		// The schema gate watches the DXP pods only, so the operator does
		// not cache every pod in the cluster.

		byObject[&corev1.Pod{}] = cache.ByObject{
			Label: labels.SelectorFromSet(
				labels.Set{schema.LabelComponent: schema.LabelComponentLiferay},
			),
		}
	}

	manager, error := controllerruntime.NewManager(
		controllerruntime.GetConfigOrDie(),
		controllerruntime.Options{
			Cache: cache.Options{
				ByObject: byObject,
			},
			HealthProbeBindAddress: config.ProbeAddress,
			Metrics: metricsserver.Options{
				BindAddress: config.MetricsAddress,
			},
			Scheme: scheme,
		},
	)

	if error != nil {
		controller.SetupLog.Error(error, "Unable to start manager")

		os.Exit(1)
	}

	if error := manager.AddHealthzCheck("healthz", healthz.Ping); error != nil {
		controller.SetupLog.Error(error, "Unable to set up health check")

		os.Exit(1)
	}

	if error := manager.AddReadyzCheck("readyz", healthz.Ping); error != nil {
		controller.SetupLog.Error(error, "Unable to set up ready check")

		os.Exit(1)
	}

	var reconcilers []controller.Reconciler

	if config.ClientExtensionsEnabled {
		reconcilers = append(
			reconcilers,
			&cx.ClientExtensionReconciler{
				APIReader:      manager.GetAPIReader(),
				Client:         manager.GetClient(),
				Recorder:       manager.GetEventRecorderFor("clientextension-controller"),
				ServiceAccount: config.OperatorNamespace + "/" + config.OperatorServiceAccount,
			},
		)
	}

	provisioningClient := provisioning.NewHTTPClient(config.ProvisioningBaseURL)

	reconcilers = append(
		reconcilers,
		&licensing.LiferayEnvironmentReconciler{
			Client:                  manager.GetClient(),
			ExpirationGracePeriod:   config.ExpirationGracePeriod,
			HeartbeatInterval:       config.HeartbeatInterval,
			MarketplaceMountPath:    config.MarketplaceMountPath,
			Provisioning:            provisioningClient,
			ProvisioningGracePeriod: config.ProvisioningGracePeriod,
			Recorder:                manager.GetEventRecorderFor("liferayenvironment-controller"),
			RetryInitialDelay:       config.RetryInitialDelay,
			RetryMaxDelay:           config.RetryMaxDelay,
			Syncer: addon.NewSyncer(
				provisioningClient, config.DownloadPollInterval,
				config.RetryInitialDelay, config.RetryMaxDelay, addon.GoRunner{},
			),
		},
	)

	if config.SchemaGateEnabled {
		clientset, error := kubernetes.NewForConfig(manager.GetConfig())

		if error != nil {
			controller.SetupLog.Error(error, "Unable to create the Kubernetes clientset")

			os.Exit(1)
		}

		reconcilers = append(
			reconcilers,
			&schema.StatefulSetReconciler{
				APIReader:           manager.GetAPIReader(),
				Client:              manager.GetClient(),
				DatabaseClientImage: config.DatabaseClientImage,
				LogReader:           &schema.ClientsetLogReader{Clientset: clientset},
				Recorder:            manager.GetEventRecorderFor("schemagate-controller"),
				ReleaseCatalog: &schema.HTTPReleaseCatalog{
					CacheDuration: time.Hour,
					URL:           config.ReleasesURL,
				},
				UpgradeChecker: &schema.GogoUpgradeChecker{
					Clientset: clientset,
					Config:    manager.GetConfig(),
				},
			},
		)
	}

	if error := controller.SetupWithManager(manager, reconcilers...); error != nil {
		controller.SetupLog.Error(error, "Unable to set up controllers")

		os.Exit(1)
	}

	controller.SetupLog.Info(
		"Starting manager",
		"clientExtensionsEnabled", config.ClientExtensionsEnabled,
		"heartbeatInterval", config.HeartbeatInterval,
		"metricsAddress", config.MetricsAddress,
		"probeAddress", config.ProbeAddress,
		"provisioningBaseURL", config.ProvisioningBaseURL,
		"schemaGateEnabled", config.SchemaGateEnabled,
	)

	if error := manager.Start(controllerruntime.SetupSignalHandler()); error != nil {
		controller.SetupLog.Error(error, "Unexpected error while running manager")

		os.Exit(1)
	}
}

type config struct {
	ClientExtensionsEnabled bool          `env:"CLIENT_EXTENSIONS_ENABLED" envDefault:"false"`
	DatabaseClientImage     string        `env:"DATABASE_CLIENT_IMAGE" envDefault:"postgres:17-alpine"`
	Debug                   bool          `env:"DEBUG" envDefault:"false"`
	DownloadPollInterval    time.Duration `env:"DOWNLOAD_POLL_INTERVAL" envDefault:"15s"`
	ExpirationGracePeriod   time.Duration `env:"EXPIRATION_GRACE_PERIOD" envDefault:"2160h"`
	HeartbeatInterval       time.Duration `env:"HEARTBEAT_INTERVAL" envDefault:"10m"`
	MarketplaceMountPath    string        `env:"MARKETPLACE_MOUNT_PATH" envDefault:"/marketplace"`
	MetricsAddress          string        `env:"METRICS_ADDRESS" envDefault:":8080"`
	OperatorNamespace       string        `env:"OPERATOR_NAMESPACE" envDefault:"dxp-operator-system"`
	OperatorServiceAccount  string        `env:"OPERATOR_SERVICE_ACCOUNT" envDefault:"dxp-operator"`
	ProbeAddress            string        `env:"PROBE_ADDRESS" envDefault:":8081"`
	ProvisioningBaseURL     string        `env:"PROVISIONING_BASE_URL" envDefault:"https://api.one.liferay.com"`
	ProvisioningGracePeriod time.Duration `env:"PROVISIONING_GRACE_PERIOD" envDefault:"168h"`
	ReleasesURL             string        `env:"RELEASES_URL" envDefault:"https://releases-cdn.liferay.com/releases.json"`
	RetryInitialDelay       time.Duration `env:"RETRY_INITIAL_DELAY" envDefault:"30s"`
	RetryMaxDelay           time.Duration `env:"RETRY_MAX_DELAY" envDefault:"30m"`
	SchemaGateEnabled       bool          `env:"SCHEMA_GATE_ENABLED" envDefault:"false"`
}

var scheme = runtime.NewScheme()
