// Copyright 2019 The Go Cloud Development Kit Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gocloud.dev/gcp"
	"gocloud.dev/internal/cmd/gocdk/internal/docker"
	"golang.org/x/xerrors"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
	cloudrun "google.golang.org/api/run/v1alpha1"
)

func registerLaunchCmd(ctx context.Context, pctx *processContext, rootCmd *cobra.Command) {
	var dockerImage string
	launchCmd := &cobra.Command{
		Use:   "launch BIOME",
		Short: "TODO Launch BIOME",
		Long:  "TODO more about launch",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return launch(ctx, pctx, args[0], dockerImage)
		},
	}
	launchCmd.Flags().StringVar(&dockerImage, "image", defaultDockerTag, "Docker image to launch in the form `name[:tag] OR :tag`")
	rootCmd.AddCommand(launchCmd)
}

func launch(ctx context.Context, pctx *processContext, biome, dockerImage string) error {
	moduleRoot, err := pctx.ModuleRoot(ctx)
	if err != nil {
		return xerrors.Errorf("gocdk launch: %w", err)
	}

	// Get the image name from the Dockerfile if not specified.
	if dockerImage == "" || strings.HasPrefix(dockerImage, ":") {
		name, err := moduleDockerImageName(moduleRoot)
		if err != nil {
			return xerrors.Errorf("gocdk launch: %w", err)
		}
		dockerImage = name + dockerImage
	}

	// Prepare the launcher.
	cfg, err := readBiomeConfig(moduleRoot, biome)
	if err != nil {
		return xerrors.Errorf("gocdk launch: %w", err)
	}
	if cfg.Launcher == nil {
		return xerrors.Errorf("gocdk launch: launcher not specified in %s", filepath.Join(biomeDir(moduleRoot, biome), biomeConfigFileName))
	}
	launcher, err := newLauncher(ctx, pctx, *cfg.Launcher)
	if err != nil {
		return xerrors.Errorf("gocdk launch: %w", err)
	}

	// Read the launch specifier from the biome's Terraform output.
	tfOutput, err := tfReadOutput(ctx,
		biomeDir(moduleRoot, biome),
		pctx.env)
	if err != nil {
		return xerrors.Errorf("gocdk launch: %w", err)
	}
	env, err := launchEnv(tfOutput)
	if err != nil {
		return xerrors.Errorf("gocdk launch: %w", err)
	}

	// Launch the application.
	launchURL, err := launcher.Launch(ctx, &LaunchInput{
		DockerImage: dockerImage,
		Env:         env,
		Specifier:   tfOutput["launch_specifier"].mapValue(),
	})
	if err != nil {
		return xerrors.Errorf("gocdk launch: %w", err)
	}
	pctx.Logf("Serving at %s\n", launchURL)
	return nil
}

// TODO(light): Move Launcher and supporting types to their own package.

// Launcher is the interface for any type that can launch a Docker image.
type Launcher interface {
	Launch(ctx context.Context, input *LaunchInput) (*url.URL, error)
}

// LaunchInput is the input to a launcher.
type LaunchInput struct {
	// DockerImage specifies the image name and tag of the local Docker image to
	// deploy. If the local image does not exist, then the launcher should return
	// an error.
	DockerImage string

	// env is the set of additional environment variables to set. It should not
	// include PORT nor should it contain multiple entries for the same variable
	// name.
	Env []string

	// specifier is the set of arguments passed from a biome's Terraform module.
	Specifier map[string]interface{}
}

// newLauncher creates the launcher for the given name.
func newLauncher(ctx context.Context, pctx *processContext, launcherName string) (Launcher, error) {
	switch launcherName {
	case "local":
		return &localLauncher{
			logger:       pctx.errlog,
			dockerClient: docker.New(pctx.env),
		}, nil
	case "cloudrun":
		creds, err := pctx.gcpCredentials(ctx)
		if err != nil {
			return nil, xerrors.Errorf("prepare cloudrun launcher: %w", err)
		}
		httpClient, _ := gcp.NewHTTPClient(http.DefaultTransport, creds.TokenSource)
		runService, err := cloudrun.NewService(ctx, option.WithHTTPClient(&httpClient.Client))
		if err != nil {
			return nil, xerrors.Errorf("prepare cloudrun launcher: %w", err)
		}
		return &cloudRunLauncher{
			logger:       pctx.errlog,
			client:       runService,
			dockerClient: docker.New(pctx.env),
		}, nil
	default:
		return nil, xerrors.Errorf("prepare launcher: unknown launcher %q", launcherName)
	}
}

// localLauncher starts local Docker containers.
type localLauncher struct {
	logger       *log.Logger
	dockerClient *docker.Client
}

// Launch implements Launcher.Launch.
func (local *localLauncher) Launch(ctx context.Context, input *LaunchInput) (*url.URL, error) {
	hostPort := specifierIntValue(input.Specifier, "host_port")
	if hostPort == 0 {
		hostPort = 8080
	} else if hostPort < 0 || hostPort > 65535 {
		return nil, xerrors.Errorf("local launch: host_port is out of range [0, 65535]")
	}
	// TODO(light): Maybe don't remove on exit?
	containerID, err := local.dockerClient.Start(ctx, input.DockerImage, &docker.RunOptions{
		Env:          append(append([]string(nil), input.Env...), "PORT=8080"),
		RemoveOnExit: true,
		Publish:      []string{fmt.Sprintf("%d:8080", hostPort)},
	})
	if err != nil {
		return nil, xerrors.Errorf("local launch: %w", err)
	}

	local.logger.Printf("Docker container %s started, waiting for healthy...", containerID)
	serveURL := &url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("localhost:%d", hostPort),
		Path:   "/",
	}
	healthCheckURL := &url.URL{
		Scheme: serveURL.Scheme,
		Host:   serveURL.Host,
		Path:   "/healthz/readiness",
	}
	if err := waitForHealthy(ctx, healthCheckURL); err != nil {
		// TODO(light): Run `docker stop`.
		return nil, xerrors.Errorf("local launch: %w", err)
	}
	local.logger.Printf("Container healthy! To shut down, run: docker stop %s", containerID)
	return serveURL, nil
}

// cloudRunLauncher pushes Docker containers to Google Container Registry and
// creates/updates a Cloud Run service.
type cloudRunLauncher struct {
	logger       *log.Logger
	client       *cloudrun.APIService
	dockerClient *docker.Client
}

// Launch implements Launcher.Launch.
func (crl *cloudRunLauncher) Launch(ctx context.Context, input *LaunchInput) (*url.URL, error) {
	projectID := specifierStringValue(input.Specifier, "project_id")
	location := specifierStringValue(input.Specifier, "location")
	serviceName := specifierStringValue(input.Specifier, "service_name")
	if projectID == "" || location == "" || serviceName == "" {
		return nil, xerrors.New("cloud run launch: launch_specifier missing project_id, location, and/or service_name")
	}

	// Push to GCR.
	imageRef, err := crl.tagForCloudRun(ctx, input.DockerImage, input.Specifier)
	if err != nil {
		return nil, xerrors.Errorf("cloud run launch: %w", err)
	}
	// TODO(light): Send docker push output somewhere.
	if err := crl.dockerClient.Push(ctx, imageRef, ioutil.Discard); err != nil {
		return nil, xerrors.Errorf("cloud run launch: %w", err)
	}

	// Launch on Cloud Run.
	var env []*cloudrun.EnvVar
	for i, inputVar := range input.Env {
		eqIdx := strings.IndexByte(inputVar, '=')
		if eqIdx == -1 {
			return nil, xerrors.Errorf("cloud run launch: environment variables should be in the form VARNAME=VALUE, but env[%d] = %q", i, inputVar)
		}
		env = append(env, &cloudrun.EnvVar{
			Name:  inputVar[:eqIdx],
			Value: inputVar[eqIdx+1:],
		})
	}
	// Reference of Knative service specifications can be found at
	// https://github.com/knative/serving/blob/master/docs/spec/spec.md#service
	// or https://cloud.google.com/run/docs/reference/rest/v1alpha1/namespaces.services#Service
	serviceMeta := &cloudrun.ObjectMeta{
		Name:      serviceName,
		Namespace: projectID,
	}
	serviceSpec := &cloudrun.ServiceSpec{
		RunLatest: &cloudrun.ServiceSpecRunLatest{
			Configuration: &cloudrun.ConfigurationSpec{
				RevisionTemplate: &cloudrun.RevisionTemplate{
					Spec: &cloudrun.RevisionSpec{
						Container: &cloudrun.Container{
							Image: imageRef,
							Env:   env,
							// TODO(light): Add liveness and readiness probes.
						},
						ContainerConcurrency: 0, // thread-safe
					},
				},
			},
		},
	}
	locationString := "projects/" + projectID + "/locations/" + location
	serviceString := locationString + "/services/" + serviceName
	createCall := crl.client.Projects.Locations.Services.Create(locationString, &cloudrun.Service{
		ApiVersion: "serving.knative.dev/v1alpha1",
		Kind:       "Service",
		Metadata:   serviceMeta,
		Spec:       serviceSpec,
	})
	_, err = createCall.Context(ctx).Do()
	if err == nil && !specifierBoolValue(input.Specifier, "internal_only") {
		// Service created for first time. Make publicly accessible.
		policyCall := crl.client.Projects.Locations.Services.SetIamPolicy(serviceString, &cloudrun.SetIamPolicyRequest{
			Policy: &cloudrun.Policy{
				Bindings: []*cloudrun.Binding{
					{
						Role:    "roles/run.invoker",
						Members: []string{"allUsers"},
					},
				},
			},
		})
		if _, err := policyCall.Context(ctx).Do(); err != nil {
			return nil, xerrors.Errorf("cloud run launch: %w")
		}
	} else if apiError := (*googleapi.Error)(nil); xerrors.As(err, &apiError) && apiError.Code == http.StatusConflict {
		// Already exists, add revision.
		replaceCall := crl.client.Projects.Locations.Services.ReplaceService(serviceString, &cloudrun.Service{
			ApiVersion: "serving.knative.dev/v1alpha1",
			Kind:       "Service",
			Metadata:   serviceMeta,
			Spec:       serviceSpec,
		})
		// Handle error below in the same way a create call is handled.
		_, err = replaceCall.Context(ctx).Do()
	}
	if err != nil {
		return nil, xerrors.Errorf("cloud run launch: %w", err)
	}
	crl.logger.Printf("Created revision, waiting for service %s to make changes...", serviceName)

	// Wait for it to become ready.
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
		case <-ctx.Done():
			return nil, xerrors.Errorf("cloud run launch: wait for ready: %w", ctx.Err())
		}

		currService, err := crl.client.Projects.Locations.Services.Get(serviceString).Context(ctx).Do()
		if err != nil {
			return nil, xerrors.Errorf("cloud run launch: wait for ready: %w", err)
		}
		if currService.Status.ObservedGeneration == currService.Metadata.Generation && conditionStatus(currService.Status.Conditions, "RoutesReady") == "True" {
			// TODO(light): According to docs, this should also check for Status
			// containing a type="Ready" condition that equals "True" or "False".
			// Instead, it seems to always be "Unknown".
			if currService.Status.Address == nil {
				return nil, xerrors.Errorf("cloud run launch: ready, but server did not return address")
			}
			// Weirdly, the hostname is a URL, not a domain name.
			u, err := url.Parse(currService.Status.Address.Hostname)
			if err != nil {
				return nil, xerrors.Errorf("cloud run launch: parse service URL: %w", err)
			}
			return u, nil
		}
		crl.logger.Println("Not ready yet, will poll again...")
	}
}

// tagForCloudRun tags the given image as needed so that running `docker push`
// will place the image in a registry accessible by Cloud Run. The returned
// string is the image reference that should be passed to Cloud Run.
func (crl *cloudRunLauncher) tagForCloudRun(ctx context.Context, imageRef string, launchSpecifier map[string]interface{}) (string, error) {
	rewrittenRef, err := imageRefForCloudRun(imageRef, launchSpecifier)
	if err != nil {
		return "", xerrors.Errorf("docker tag: %w")
	}
	if rewrittenRef == imageRef {
		return rewrittenRef, nil
	}
	if err := crl.dockerClient.Tag(ctx, imageRef, rewrittenRef); err != nil {
		return "", xerrors.Errorf("docker tag: %w", err)
	}
	return rewrittenRef, nil
}

// imageRefForCloudRun computes the image reference needed to launch the given
// local image reference on gcr.io and the launch specifier. If the returned
// string is equal to localImage, then no retagging is necessary before pushing.
func imageRefForCloudRun(localImage string, launchSpecifier map[string]interface{}) (string, error) {
	name, tag, digest := docker.ParseImageRef(localImage)
	if tag == ":" {
		return "", xerrors.Errorf("determine image name for Cloud Run: empty tag in %q", localImage)
	}
	if specName := specifierStringValue(launchSpecifier, "image_name"); specName != "" {
		// First, use image name from launch specifier if present.
		if !isGCRName(specName) {
			return "", xerrors.Errorf("determine image name for Cloud Run: launch specifier image_name = %q, not a gcr.io name", specName)
		}
		name = specName
	} else if !isGCRName(name) {
		// Otherwise, if the image name does not have a gcr.io prefix, then prepend it.
		project := specifierStringValue(launchSpecifier, "project_id")
		if project == "" {
			return "", xerrors.New("determine image name for Cloud Run: launch specifier project_id empty")
		}
		// TODO(light): This can be wrong for ORG:PROJECT project IDs, but those
		// are deprecated anyway.
		name = "gcr.io/" + project + "/" + name
	}
	return name + tag + digest, nil
}

// isGCRName reports whether the given image name or reference identifies an
// image on Google Container Registry.
//
// The acceptable host names are documented here:
// https://cloud.google.com/container-registry/docs/pushing-and-pulling#tag_the_local_image_with_the_registry_name
func isGCRName(image string) bool {
	prefixes := []string{
		"gcr.io/",
		"us.gcr.io/",
		"eu.gcr.io/",
		"asia.gcr.io/",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(image, p) {
			return true
		}
	}
	return false
}

// conditionStatus finds the Cloud Run condition with the given name and returns
// its status string (one of "True", "False", or "Unknown") or empty string if
// the condition was not found.
func conditionStatus(conds []*cloudrun.ServiceCondition, name string) string {
	for _, c := range conds {
		if c.Type == name {
			return c.Status
		}
	}
	return ""
}

// specifierStringValue returns the specifier's value for a key if it is a string.
func specifierStringValue(spec map[string]interface{}, key string) string {
	v, _ := spec[key].(string)
	return v
}

// specifierIntValue returns the specifier's value for a key if it is an integer.
func specifierIntValue(spec map[string]interface{}, key string) int {
	switch v := spec[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		i, _ := v.Int64()
		return int(i)
	case string:
		i, _ := strconv.ParseInt(v, 10, 0)
		return int(i)
	default:
		return 0
	}
}

// specifierBoolValue returns the specifier's value for a key if it is a boolean.
func specifierBoolValue(spec map[string]interface{}, key string) bool {
	switch v := spec[key].(type) {
	case bool:
		return v
	case string:
		b, _ := strconv.ParseBool(v)
		return b
	default:
		return false
	}
}
