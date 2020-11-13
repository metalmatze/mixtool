// Copyright 2018 mixtool authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"

	"github.com/monitoring-mixins/mixtool/pkg/jsonnetbundler"
	"github.com/monitoring-mixins/mixtool/pkg/mixer"

	"github.com/urfave/cli"
)

// type mixin struct {
// 	URL         string `json:"source"`
// 	Description string `json:"description,omitempty"`
// 	Name        string `json:"name"`
// 	Subdir      string `json:"subdir"`
// }

func installCommand() cli.Command {
	return cli.Command{
		Name:        "install",
		Usage:       "Install a mixin",
		Description: "Install a mixin from a repository",
		Action:      installAction,
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:  "bind-address",
				Usage: "Address to bind HTTP server to.",
				Value: "https://127.0.0.1:8080",
			},
			cli.StringFlag{
				Name:  "rule-file",
				Usage: "File to provision rules into.",
			},
			cli.StringFlag{
				Name:  "directory, d",
				Usage: "Path where the downloaded mixin is saved. If it doesn't exist already it will be created",
			},
		},
	}
}

// Downloads a mixin from a given repository given by url and places into directory
// by running jb init and jb install
func downloadMixin(url string, directory string) error {
	// intialize the jsonnet bundler library
	err := jsonnetbundler.InitCommand(directory)
	if err != nil {
		return err
	}

	// use vendor directory by default
	// by default, set the single flag on
	err = jsonnetbundler.InstallCommand(directory, "vendor", []string{url}, false)
	if err != nil {
		fmt.Println(err)
		return err
	}

	return nil
}

// Gets mixins from default website - mostly copied from list.go
func getMixins() ([]mixin, error) {
	body, err := queryWebsite(defaultWebsite)
	if err != nil {
		return nil, err
	}
	mixins, err := parseMixinJSON(body)
	if err != nil {
		return nil, err
	}
	return mixins, nil
}

func generateMixin(directory string, mixinURL string, options mixer.GenerateOptions) error {
	fmt.Println("running generate all")

	err := os.Chdir(directory)
	if err != nil {
		return fmt.Errorf("Cannot cd into directory %s", err)
	}

	files, err := filepath.Glob("*")
	fmt.Println("in generatemixin, directory is ", directory, files)

	// create a temporary jsonnet file that
	// imports mixin.libsonnet as the "main" file in the mixin configfuration
	// then run generate all passing in the vendor folder to find dependencies
	// the name of the mixin folder inside vendor seems to be the last fragment of mixin's url + subdir
	// TODO: need to get absolute path of the mixin
	fragment := filepath.Base(mixinURL)
	tempContent := fmt.Sprintf("import \"%s\"", filepath.Join(fragment, "mixin.libsonnet"))
	err = ioutil.WriteFile("temp.jsonnet", []byte(tempContent), 0644)
	if err != nil {
		return err
	}

	err = generateAll("temp.jsonnet", options)
	if err != nil {
		return err
	}

	err = os.Remove("temp.jsonnet")
	if err != nil {
		return err
	}
	return nil
}

func putMixin(directory string, mixinURL string, bindAddress string, options mixer.GenerateOptions) error {
	fmt.Println("running put Mixin")

	wd, err := os.Getwd()
	fmt.Println("path is", wd)
	if err != nil {
		return err
	}

	// err := os.Chdir(filepath.Join()
	if err != nil {
		return fmt.Errorf("Cannot cd into directory %s", err)
	}

	// // alerts.yaml
	// alertsFilename := options.AlertsFilename
	// alertsReader, err := os.Open(alertsFilename)
	// if err != nil {
	// 	return err
	// }

	// rules.yaml
	rulesFilename := options.RulesFilename
	rulesReader, err := os.Open(rulesFilename)
	if err != nil {
		return err
	}

	u, err := url.Parse(bindAddress)
	if err != nil {
		return err
	}
	u.Path = path.Join(u.Path, "/api/v1/rules")

	req, err := http.NewRequest("PUT", u.String(), rulesReader)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode == 200 {
		fmt.Println("OK")
	} else {
		fmt.Printf("resp is %v\n", resp.Body)
	}

	if err != nil {
		return err
	}

	return nil
}

func installAction(c *cli.Context) error {
	directory := c.String("directory")
	if directory == "" {
		return fmt.Errorf("Must specify a directory to download mixin")
	}

	_, err := os.Stat(directory)
	if os.IsNotExist(err) {
		err = os.MkdirAll(directory, 0755)
		if err != nil {
			return err
		}
	}

	mixinPath := c.Args().First()
	if mixinPath == "" {
		return fmt.Errorf("Expected the url of mixin repository or name of the mixin. Show available mixins using mixtool list")
	}

	mixinsList, err := getMixins()
	if err != nil {
		return err
	}

	var mixinURL string
	if _, err := url.ParseRequestURI(mixinPath); err != nil {
		// check if the name exists in mixinsList
		found := false
		for _, m := range mixinsList {
			if m.Name == mixinPath {
				// join paths together
				u, err := url.Parse(m.URL)
				if err != nil {
					return err
				}
				u.Path = path.Join(u.Path, m.Subdir)
				mixinURL = u.String()
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("Could not find mixin with name %s", mixinPath)
		}
	} else {
		mixinURL = mixinPath
	}

	if mixinURL == "" {
		return fmt.Errorf("Empty mixinURL")
	}

	err = downloadMixin(mixinURL, directory)
	if err != nil {
		fmt.Println(err)
		return err
	}

	generateCfg := mixer.GenerateOptions{
		AlertsFilename: "alerts.yaml",
		RulesFilename:  "rules.yaml",
		Directory:      "dashboards_out",
		JPaths:         []string{"./vendor"},
		YAML:           true,
	}

	err = generateMixin(directory, mixinURL, generateCfg)
	if err != nil {
		return err
	}

	bindAddress := c.String("bind-address")
	// run put requests onto the server
	err = putMixin(directory, mixinURL, bindAddress, generateCfg)
	if err != nil {
		return err
	}

	return nil
}