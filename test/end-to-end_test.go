package test

import (
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/joel-ling/alduin/test/pkg/clients"
	"github.com/joel-ling/alduin/test/pkg/clusters"
	"github.com/joel-ling/alduin/test/pkg/deployments"
	"github.com/joel-ling/alduin/test/pkg/images"
	"github.com/joel-ling/alduin/test/pkg/permissions"
	"github.com/joel-ling/alduin/test/pkg/repositories"
)

func TestEndToEnd(t *testing.T) {
	// As a Kubernetes administrator deploying container applications to a
	// cluster, I want a rolling restart of a deployment to be automatically
	// triggered whenever the tag of a currently-deployed image is inherited by
	// another image so that the deployment is always up-to-date without manual
	// intervention.

	var (
		e error
	)

	// Given there is a container image repository

	const (
		repositoryPort = 5000
	)

	var (
		repository        *repositories.DockerRegistry
		repositoryAddress net.TCPAddr
	)

	repositoryAddress = net.TCPAddr{
		Port: repositoryPort,
	}

	repository, e = repositories.NewDockerRegistry(repositoryAddress)
	if e != nil {
		t.Error(e)
	}

	defer repository.Destroy()

	// And in the repository there is an image of a container
	// And the container is a HTTP server with a GET endpoint
	// And the endpoint responds to requests with a fixed HTTP status code
	// And the status code is preset via a container image build argument

	const (
		buildContextPath = ".."
		dockerfilePath0  = "test/build/http-status-code-server/Dockerfile"

		localhost = "127.0.0.1"

		imageName0     = "http-status-code-server"
		imageRefFormat = "%s/%s"

		serverPort    = 8000
		serverPortKey = "SERVER_PORT"

		statusCode0   = http.StatusNoContent
		statusCodeKey = "STATUS_CODE"
	)

	var (
		image0    *images.DockerImage
		imageRef0 string
	)

	image0, e = images.NewDockerImage(buildContextPath, dockerfilePath0)
	if e != nil {
		t.Error(e)
	}

	repositoryAddress.IP = net.ParseIP(localhost)

	imageRef0 = fmt.Sprintf(imageRefFormat,
		repositoryAddress.String(),
		imageName0,
	)

	image0.SetTag(imageRef0)

	image0.SetBuildArg(serverPortKey,
		fmt.Sprint(serverPort),
	)

	image0.SetBuildArg(statusCodeKey,
		fmt.Sprint(statusCode0),
	)

	e = image0.Build(os.Stderr)
	if e != nil {
		t.Error(e)
	}

	e = image0.Push(os.Stderr)
	if e != nil {
		t.Error(e)
	}

	// And there is a Kubernetes cluster

	const (
		// See https://pkg.go.dev/io/ioutil#TempFile
		kubeconfigDirectory = ""
		kubeconfigFilename  = "*"

		clusterName  = "test-cluster"
		nodeImageRef = "kindest/node:v1.23.3"

		dockerHost = "172.17.0.1"
	)

	var (
		cluster        *clusters.KindCluster
		kubeconfigFile *os.File
	)

	kubeconfigFile, e = ioutil.TempFile(
		kubeconfigDirectory,
		kubeconfigFilename,
	)
	if e != nil {
		t.Error(e)
	}

	defer os.Remove(
		kubeconfigFile.Name(),
	)

	cluster, e = clusters.NewKindCluster(
		nodeImageRef,
		clusterName,
		kubeconfigFile.Name(),
	)
	if e != nil {
		t.Error(e)
	}

	cluster.AddPortMapping(serverPort)

	repositoryAddress.IP = net.ParseIP(dockerHost)

	cluster.AddHTTPRegistryMirror(repositoryAddress)

	e = cluster.Create()
	if e != nil {
		t.Error(e)
	}

	defer cluster.Destroy()

	// And the server is deployed to the cluster using a Kubernetes deployment
	// And the image of the server is pulled from the repository
	// And the endpoint is exposed using a Kubernetes service

	const (
		serviceAccountName = ""

		deploymentLabelKey = "app"
	)

	var (
		deployment0 *deployments.KubernetesDeployment
	)

	deployment0, e = deployments.NewKubernetesDeployment(
		imageName0,
		serviceAccountName,
		kubeconfigFile.Name(),
	)
	if e != nil {
		t.Error(e)
	}

	deployment0.SetLabel(deploymentLabelKey, imageName0)

	deployment0.AddContainerWithSingleTCPPort(
		imageName0,
		strings.ReplaceAll(imageRef0, localhost, dockerHost),
		serverPort,
	)

	e = deployment0.Create()
	if e != nil {
		t.Error(e)
	}

	defer deployment0.Delete()

	const (
		scheme   = "http"
		timeout0 = time.Second
	)

	var (
		client        *clients.HTTPClient
		endpoint      url.URL
		serverAddress net.TCPAddr
		status        int
	)

	client, e = clients.NewHTTPClient()
	if e != nil {
		t.Error(e)
	}

	serverAddress.Port = serverPort

	endpoint = url.URL{
		Scheme: scheme,
		Host:   serverAddress.String(),
	}

	status, e = client.GetStatusCodeFromEndpoint(endpoint, timeout0)
	if e != nil {
		t.Error(e)
	}

	assert.EqualValues(t, statusCode0, status)

	// And Alduin is running in the cluster
	// And Alduin is authenticated as a Kubernetes service account
	// And the service account is authorised to get and patch deployments

	const (
		dockerfilePath1 = ""

		imageName1 = "alduin"
	)

	var (
		image1    *images.DockerImage
		imageRef1 string
	)

	image1, e = images.NewDockerImage(buildContextPath, dockerfilePath1)
	if e != nil {
		t.Error(e)
	}

	repositoryAddress.IP = net.ParseIP(localhost)

	imageRef1 = fmt.Sprintf(imageRefFormat,
		repositoryAddress.String(),
		imageName1,
	)

	image1.SetTag(imageRef1)

	e = image1.Build(os.Stderr)
	if e != nil {
		t.Error(e)
	}

	e = image1.Push(os.Stderr)
	if e != nil {
		t.Error(e)
	}

	e = image1.Remove()
	if e != nil {
		t.Error(e)
	}

	const (
		resource = "deployments"
		verb0    = "get"
		verb1    = "patch"
	)

	var (
		permission *permissions.KubernetesRole
	)

	permission, e = permissions.NewKubernetesRole(
		imageName1,
		kubeconfigFile.Name(),
	)
	if e != nil {
		t.Error(e)
	}

	permission.AddPolicyRule(
		[]string{verb0, verb1},
		[]string{resource},
	)

	e = permission.Create()
	if e != nil {
		t.Error(e)
	}

	var (
		deployment1 *deployments.KubernetesDeployment
	)

	deployment1, e = deployments.NewKubernetesDeployment(
		imageName1,
		imageName1,
		kubeconfigFile.Name(),
	)
	if e != nil {
		t.Error(e)
	}

	deployment1.SetLabel(deploymentLabelKey, imageName1)

	deployment1.AddContainerWithoutPorts(
		imageName1,
		strings.ReplaceAll(imageRef1, localhost, dockerHost),
	)

	e = deployment1.Create()
	if e != nil {
		t.Error(e)
	}

	defer deployment1.Delete()

	// When I rebuild the image so that it returns a different status code
	// And I transfer to the new image the tag of the existing image
	// And I push the new image to the repository

	const (
		statusCode1 = http.StatusTeapot
	)

	image0.SetBuildArg(statusCodeKey,
		fmt.Sprint(statusCode1),
	)

	e = image0.Build(os.Stderr)
	if e != nil {
		t.Error(e)
	}

	e = image0.Push(os.Stderr)
	if e != nil {
		t.Error(e)
	}

	e = image0.Remove()
	if e != nil {
		t.Error(e)
	}

	// And I allow time for a rolling restart of the deployment to complete
	// And I send a request to the endpoint
	// Then I should see in the response to the request the new status code

	const (
		timeout1 = time.Minute
	)

	assert.Eventually(t,
		func() bool {
			status, e = client.GetStatusCodeFromEndpoint(endpoint, timeout0)
			if e != nil {
				t.Error(e)
			}

			return (status == statusCode1)
		},
		timeout1,
		timeout0,
	)
}