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
	"bytes"
	"context"
	"io/ioutil"
	"os"
	slashpath "path"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/spf13/cobra"
	"gocloud.dev/internal/cmd/gocdk/static"
	"golang.org/x/xerrors"
)

func registerInitCmd(ctx context.Context, pctx *processContext, rootCmd *cobra.Command) {
	var modpath string
	var allowExistingDir bool
	initCmd := &cobra.Command{
		Use:   "init PATH_TO_PROJECT_DIR",
		Short: "TODO: Initialize a new project",
		Long:  "TODO more about init",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return doInit(ctx, pctx, args[0], modpath, allowExistingDir)
		},
	}
	initCmd.Flags().StringVarP(&modpath, "module-path", "m", "", "the module import path for your project's go.mod file (required if project is outside of GOPATH)")
	// TODO(#1918): Remove this flag when empty directories are allowed; it is
	// currently used to enabled tests to create an empty tempdir and then run
	// "init" on it.
	initCmd.Flags().BoolVar(&allowExistingDir, "allow-existing-dir", false, "true to allow initializing an existing directory (contents may be overwritten!)")
	rootCmd.AddCommand(initCmd)
}

func doInit(ctx context.Context, pctx *processContext, dir, modpath string, allowExistingDir bool) error {
	projectDir := pctx.resolve(dir)
	if modpath == "" {
		var err error
		modpath, err = inferModulePath(ctx, pctx, projectDir)
		if err != nil {
			// TODO(clausti): return information about how to mitigate this error
			// e.g. tell them to use --module-path to specify it
			return xerrors.Errorf("gocdk init: %w", err)
		}
	}

	// TODO(#1918): allow an existing empty directory, for some definition of empty.
	if _, err := os.Stat(projectDir); err == nil {
		if !allowExistingDir {
			return xerrors.Errorf("gocdk init: %s already exists", projectDir)
		}
	} else if !os.IsNotExist(err) {
		return xerrors.Errorf("gocdk init: %w", err)
	}

	tmplValues := struct {
		ProjectName string
		ModulePath  string
	}{
		ProjectName: filepath.Base(projectDir),
		ModulePath:  modpath,
	}
	if err := materializeTemplateDir(projectDir, "init", tmplValues); err != nil {
		return xerrors.Errorf("gocdk init: %w", err)
	}
	pctx.Logf("Project created at %s with:\n", projectDir)
	pctx.Logf("- Go HTTP server")
	pctx.Logf("- Dockerfile")
	pctx.Logf("- 'dev' biome for local development settings")
	pctx.Logf("Run `cd %s`, then run:\n", dir)
	pctx.Logf("- `gocdk serve` to run the server locally with live code reloading")
	pctx.Logf("- `gocdk demo` to test new APIs")
	pctx.Logf("- `gocdk build` to build a Docker container")
	pctx.Logf("- `gocdk biome add` to configure launch settings")
	return nil
}

// TODO(rvangent): Consider having this log all of the file(s) it adds?
func materializeTemplateDir(dst string, srcRoot string, data interface{}) error {
	dir, err := static.Open(srcRoot)
	if err != nil {
		return xerrors.Errorf("materialize %s at %s: %w", srcRoot, dst, err)
	}
	infos, err := dir.Readdir(-1)
	dir.Close()
	if err != nil {
		return xerrors.Errorf("materialize %s at %s: %w", srcRoot, dst, err)
	}
	if err := os.MkdirAll(dst, 0777); err != nil {
		return xerrors.Errorf("materialize %s at %s: %w", srcRoot, dst, err)
	}
	for _, info := range infos {
		name := info.Name()
		currDst := filepath.Join(dst, name)
		currSrc := slashpath.Join(srcRoot, name)
		if info.IsDir() {
			if err := materializeTemplateDir(currDst, currSrc, data); err != nil {
				return err
			}
			continue
		}
		f, err := static.Open(currSrc)
		if err != nil {
			return xerrors.Errorf("materialize %s at %s: %w", currSrc, currDst, err)
		}
		templateSource, err := ioutil.ReadAll(f)
		f.Close()
		if err != nil {
			return xerrors.Errorf("materialize %s at %s: %w", currSrc, currDst, err)
		}
		tmpl, err := template.New(name).Parse(string(templateSource))
		if err != nil {
			return xerrors.Errorf("materialize %s at %s: %w", currSrc, currDst, err)
		}
		buf := new(bytes.Buffer)
		if err := tmpl.Execute(buf, data); err != nil {
			return xerrors.Errorf("materialize %s at %s: %w", currSrc, currDst, err)
		}
		if err := ioutil.WriteFile(currDst, buf.Bytes(), 0666); err != nil {
			return xerrors.Errorf("materialize %s at %s: %w", currSrc, currDst, err)
		}
	}
	return nil
}

// inferModulePath will check the default GOPATH to attempt to infer the module
// import path for the project.
func inferModulePath(ctx context.Context, pctx *processContext, projectDir string) (string, error) {
	// TODO(issue #2016): Add tests for init behavior when module-path is not given.
	cmd := pctx.NewCommand(ctx, "", "go", "env", "GOPATH")
	// Since we're going to call Output, we need to make sure cmd.Stdout is nil
	// so Output can collect stdout.
	cmd.Stdout = nil
	gopath, err := cmd.Output()
	if err != nil {
		return "", xerrors.Errorf("infer module path: %w", err)
	}
	// Check if the projectDir is relative to GOPATH.
	rel, err := filepath.Rel(strings.TrimSuffix(string(gopath), "\n"), projectDir)
	if err != nil {
		return "", xerrors.Errorf("infer module path: %w", err)
	}
	inGOPATH := !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	if !inGOPATH {
		// If the project dir is outside of GOPATH, we can't infer the module import path.
		return "", xerrors.Errorf("infer module path: %s not in GOPATH", projectDir)
	}
	return filepath.ToSlash(rel), nil
}
