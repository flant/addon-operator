package helm

import (
	"net/http"

	klient "github.com/flant/kube-client/client"
	log "github.com/sirupsen/logrus"

	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/helm/client"
	"github.com/flant/addon-operator/pkg/helm/helm3"
	"github.com/flant/addon-operator/pkg/helm/helm3lib"
)

type Helm struct {
	kubeClient     klient.Client
	healthzHandler func(writer http.ResponseWriter, request *http.Request)
	newClient      func(logLabels ...map[string]string) client.HelmClient
}

func New() *Helm {
	return &Helm{}
}

func (h *Helm) WithKubeClient(kubeClient klient.Client) {
	h.kubeClient = kubeClient
}

func (h *Helm) NewClient(logLabels ...map[string]string) client.HelmClient {
	if h.newClient != nil {
		return h.newClient(logLabels...)
	}
	return nil
}

func (h *Helm) HealthzHandler() func(writer http.ResponseWriter, request *http.Request) {
	return h.healthzHandler
}

func (h *Helm) Init() error {
	helmVersion, err := DetectHelmVersion()
	if err != nil {
		return err
	}

	switch helmVersion {
	case Helm3Lib:
		log.Info("Helm3Lib detected. Use builtin Helm.")
		h.newClient = helm3lib.NewClient
		err = helm3lib.Init(&helm3lib.Options{
			Namespace:  app.Namespace,
			HistoryMax: app.Helm3HistoryMax,
			Timeout:    app.Helm3Timeout,
			KubeClient: h.kubeClient,
		})
		return err

	case Helm3:
		log.Infof("Helm 3 detected (path is '%s')", helm3.Helm3Path)
		// Use helm3 client.
		h.newClient = helm3.NewClient
		err = helm3.Init(&helm3.Helm3Options{
			Namespace:  app.Namespace,
			HistoryMax: app.Helm3HistoryMax,
			Timeout:    app.Helm3Timeout,
			KubeClient: h.kubeClient,
		})
		return err
	}

	return nil
}
