package docker_registry_manager

import (
	"os"
	"time"

	registryclient "github.com/flant/docker-registry-client/registry"
	"github.com/romana/rlog"

	"github.com/flant/antiopa/kube"
)

type DockerRegistryManager interface {
	SetErrorCallback(errorCb func())
	Run()
}

var (
	// ImageUpdated contains the new image ID (image digest) with the same name.
	ImageUpdated chan string
)

type MainRegistryManager struct {
	AntiopaImageDigest string
	AntiopaImageName   string
	AntiopaImageInfo   DockerImageInfo
	PodHostname        string
	// DockerRegistry is the object to access to a Docker registry.
	DockerRegistry *registryclient.Registry
	// ErrorCounter contains the number of Docker registry access errors.
	ErrorCounter int
	// ErrorCallback function called if an error occurs.
	ErrorCallback func()
}

// TODO данные для доступа к registry серверам нужно хранить в secret-ах.
// TODO по imageInfo.Registry брать данные и подключаться к нужному registry.
// Пока известно, что будет только registry.gitlab.company.com
var DockerRegistryInfo = map[string]map[string]string{
	"registry.gitlab.company.com": map[string]string{
		"url":      "https://registry.gitlab.company.com",
		"user":     "oauth2",
		"password": "qweqwe",
	},
	// minikube specific
	"localhost:5000": map[string]string{
		"url": "http://kube-registry.kube-system.svc.cluster.local:5000",
	},
}

// InitRegistryManager gets the image name from the POD name and requests the if of this image.
// TODO вытащить token и host в секрет
func Init(hostname string) (DockerRegistryManager, error) {
	if kube.IsRunningOutOfKubeCluster() {
		rlog.Infof("Antiopa is running out of cluster. No registry manager required.")
		return nil, nil
	}

	rlog.Debug("Init registry manager")

	// TODO Пока для доступа к registry.gitlab.company.com передаётся временный токен через переменную среды
	GitlabToken := os.Getenv("GITLAB_TOKEN")
	DockerRegistryInfo["registry.gitlab.company.com"]["password"] = GitlabToken

	ImageUpdated = make(chan string)

	rlog.Infof("Registry manager initialized")

	return &MainRegistryManager{
		ErrorCounter: 0,
		PodHostname:  hostname,
	}, nil
}

// Run runs a check every 10 seconds to detect if the image ID has changed.
func (rm *MainRegistryManager) Run() {
	if kube.IsRunningOutOfKubeCluster() {
		return
	}

	rlog.Infof("Registry manager: start")

	ticker := time.NewTicker(time.Duration(10) * time.Second)

	rm.CheckIsImageUpdated()
	for {
		select {
		case <-ticker.C:
			rm.CheckIsImageUpdated()
		}
	}
}

func (rm *MainRegistryManager) SetErrorCallback(errorCb func()) {
	rm.ErrorCallback = errorCb
}

// CheckIsImageUpdated is the main method to check for image updates.
// It starts periodically. First, it tries to connect to the the K8S API server,
// and get the name and digest of the image by the name of the Pod.
// When it receives the image digest, it queries Docker registry by the POD name
// to get the image digest in the registry. If image digest in the registry has changed,
// it sends the new digest to the channel.
func (rm *MainRegistryManager) CheckIsImageUpdated() {
	// First step. Gets image name and digest from the K8S.
	// K8S API server may be unavailable, therefore you need to connect to it periodically.
	if rm.AntiopaImageName == "" {
		rlog.Debugf("Registry manager: retrieve image name and id from kube-api")
		podImageName, podImageId := kube.KubeGetPodImageInfo(rm.PodHostname)
		if podImageName == "" {
			rlog.Debugf("Registry manager: error retrieving image name and id from kube-api. Will try again")
			return
		}

		var err error
		rm.AntiopaImageInfo, err = DockerParseImageName(podImageName)
		if err != nil {
			// A very unlikely situation because the Pod has started but its image name isn't parsed.
			rlog.Errorf("Registry manager: pod image name '%s' is invalid. Will try again. Error was: %v", podImageName, err)
			return
		}

		rm.AntiopaImageName = podImageName
		rm.AntiopaImageDigest, err = FindImageDigest(podImageId)
		if err != nil {
			rlog.Errorf("RegistryManager: %s", err)
			ImageUpdated <- "NO_DIGEST_FOUND"
			return
		}
	}

	// Second step. Starts to monitor image ID changes is the Docker registry.
	// The Docker registry also may be unavailable.
	if rm.DockerRegistry == nil {
		rlog.Debugf("Registry manager: create docker registry client")
		var url, user, password string
		if info, hasInfo := DockerRegistryInfo[rm.AntiopaImageInfo.Registry]; hasInfo {
			url = info["url"]
			user = info["user"]
			password = info["password"]
		}
		// Creates one instance of a Docker registry client.
		rm.DockerRegistry = NewDockerRegistry(url, user, password)
	}

	rlog.Debugf("Registry manager: checking registry for updates")
	digest, err := DockerRegistryGetImageDigest(rm.AntiopaImageInfo, rm.DockerRegistry)
	rm.SetOrCheckAntiopaImageDigest(digest, err)
}

// SetOrCheckAntiopaImageDigest compares the image digest with the image digest obtained from the registry.
// If they are different — sends image digest from registry to the channel.
// If digest wasn't stored - stores it.
// If there was an error in the request from the registry, then increases the error count.
// Displays an error and resets the counter if there are 3 consecutive errors.
func (rm *MainRegistryManager) SetOrCheckAntiopaImageDigest(digest string, err error) {
	if err != nil || !IsValidImageDigest(digest) {
		rm.ErrorCallback()
		rm.ErrorCounter++
		if rm.ErrorCounter >= 3 {
			rlog.Errorf("Registry manager: registry request error: %s", err)
			rm.ErrorCounter = 0
		}
		return
	}
	if digest != rm.AntiopaImageDigest {
		ImageUpdated <- digest
	}
	rm.ErrorCounter = 0
}
