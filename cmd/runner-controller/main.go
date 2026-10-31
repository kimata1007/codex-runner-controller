package main

import (
	"flag"
	"os"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	v1alpha1 "github.com/kimata1007/codex-runner-controller/api/v1alpha1"
	"github.com/kimata1007/codex-runner-controller/internal/controller"
)

func main() {
	var metricsAddr string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "address for controller metrics")
	flag.Parse()
	_ = metricsAddr

	logger := zap.New(zap.UseDevMode(true))
	ctrl.SetLogger(logger)

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		logger.Error(err, "add core scheme")
		os.Exit(1)
	}
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		logger.Error(err, "add CodexSession scheme")
		os.Exit(1)
	}
	mgr, err := manager.New(ctrl.GetConfigOrDie(), manager.Options{Scheme: scheme})
	if err != nil {
		logger.Error(err, "create manager")
		os.Exit(1)
	}
	if err := (&controller.CodexSessionController{Client: mgr.GetClient()}).SetupWithManager(mgr); err != nil {
		logger.Error(err, "setup controller")
		os.Exit(1)
	}
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		logger.Error(err, "start manager")
		os.Exit(1)
	}
}
