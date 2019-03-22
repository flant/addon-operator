package docker_registry_manager

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/docker/distribution/reference"
	registryclient "github.com/flant/docker-registry-client/registry"
	"github.com/romana/rlog"
)

type DockerImageInfo struct {
	Registry   string // Docker registry URL
	Repository string // Antiopa repository in the Registry
	Tag        string // Repository TAG (master, stable, ea, etc.)
	FullName   string // The image full name for the logging
}

// Regex to determine a valid Docker image ID (image digest).
var DockerImageDigestRe = regexp.MustCompile("(sha256:?)?[a-fA-F0-9]{64}")
var KubeDigestRe = regexp.MustCompile("docker-pullable://.*@sha256:[a-fA-F0-9]{64}")

//var KubeImageIdRe = regexp.MustCompile("docker://sha256:[a-fA-F0-9]{64}")

// Sends the request to the Docker registry and gets the image digest from the response.
// Report to the log and returns an empty string, if an errors occurs.
// Must be called in a loop until a registry responds successfully.
//
// Request to a registry may panic. The problem is somewhere in the
// 'docker-registry-client', but it happens very rarely (perhaps, the reason is
// the unavailability of a registry from K8S). Simply catches panic and writes debug message.
func DockerRegistryGetImageDigest(imageInfo DockerImageInfo, dockerRegistry *registryclient.Registry) (digest string, err error) {
	defer func() {
		if r := recover(); r != nil {
			rlog.Debugf("REGISTRY: manifest digest request panic: %s", r)
		}
	}()

	// Gets the image ID (image digest).
	imageDigest, err := dockerRegistry.ManifestDigestV2(imageInfo.Repository, imageInfo.Tag)
	if err != nil {
		rlog.Debugf("REGISTRY: manifest digest request error for %s/%s:%s: %v", imageInfo.Registry, imageInfo.Repository, imageInfo.Tag, err)
		return "", err
	}

	digest = imageDigest.String()
	rlog.Debugf("REGISTRY: imageDigest='%s' for %s:%s", digest, imageInfo.Repository, imageInfo.Tag)

	return digest, nil
}

func DockerParseImageName(imageName string) (imageInfo DockerImageInfo, err error) {
	namedRef, err := reference.ParseNormalizedNamed(imageName)
	switch {
	case err != nil:
		return
	case reference.IsNameOnly(namedRef):
		// If the image name is not tagged, Docker adds the 'latest' tag.
		namedRef = reference.TagNameOnly(namedRef)
	}

	tag := ""
	if tagged, ok := namedRef.(reference.Tagged); ok {
		tag = tagged.Tag()
	}

	imageInfo = DockerImageInfo{
		Registry:   reference.Domain(namedRef),
		Repository: reference.Path(namedRef),
		Tag:        tag,
		FullName:   imageName,
	}

	rlog.Debugf("REGISTRY image %s parsed to reg=%s repo=%s tag=%s", imageName, imageInfo.Registry, imageInfo.Repository, imageInfo.Tag)

	return
}

// FindImageDigest searches the digest in the string.
// Tooks into account the specificity of the K8S — if there is a 'docker-pullable://' prefix, then it is the image digest.
// If there is a 'docker://' prefix of a prefix is absent, then it is an image ID which cannott be used.
// Example of the string with the images digest from K8S: docker-pullable://registry/repo:tag@sha256:DIGEST-HASH
func FindImageDigest(imageId string) (image string, err error) {
	if !KubeDigestRe.MatchString(imageId) {
		err = fmt.Errorf("Pod status contains image_id and not digest. Antiopa update process not working in clusters with Docker 1.11 or earlier.")
	}
	image = DockerImageDigestRe.FindString(imageId)
	return
}

// IsValidImageDigest checks that string is an Docker image digest.
func IsValidImageDigest(imageId string) bool {
	return DockerImageDigestRe.MatchString(imageId)
}

func RegistryClientLogCallback(format string, args ...interface{}) {
	rlog.Debugf(format, args...)
}

// NewDockerRegistry - manual client constructor (as recommended in review to the registryclient.New).
// It doesn't start registry.Ping and logs events through the rlog.
func NewDockerRegistry(registryUrl, username, password string) *registryclient.Registry {
	url := strings.TrimSuffix(registryUrl, "/")

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
			DualStack: true,
		}).DialContext,
		MaxIdleConns:          1,
		MaxIdleConnsPerHost:   1,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSNextProto:          make(map[string]func(string, *tls.Conn) http.RoundTripper),
	}

	wrappedTransport := registryclient.WrapTransport(transport, url, username, password)

	return &registryclient.Registry{
		URL: url,
		Client: &http.Client{
			Transport: wrappedTransport,
		},
		Logf: RegistryClientLogCallback,
	}
}
