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
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"

	"github.com/pkg/errors"
	"github.com/urfave/cli"
)

func serverCommand() cli.Command {
	return cli.Command{
		Name:        "server",
		Usage:       "Start a server to provision Prometheus rule file(s) with.",
		Description: "Start a server to provision Prometheus rule file(s) with.",
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:  "bind-address",
				Usage: "Address to bind HTTP server to.",
			},
			cli.StringFlag{
				Name:  "prometheus-reload-url",
				Value: "http://127.0.0.1:9090/-/reload",
				Usage: "Prometheus address to reload after provisioning the rule file(s).",
			},
			cli.StringFlag{
				Name:  "rule-file",
				Usage: "File to provision rules into.",
			},
		},
		Action: serverAction,
	}
}

func serverAction(c *cli.Context) error {
	bindAddress := c.String("bind-address")
	http.Handle("/api/v1/rules", &ruleProvisioningHandler{
		ruleProvisioner: &ruleProvisioner{
			ruleFile: c.String("rule-file"),
		},
		prometheusReloader: &prometheusReloader{
			prometheusReloadURL: c.String("prometheus-reload-url"),
		},
	})
	return http.ListenAndServe(bindAddress, nil)
}

type ruleProvisioningHandler struct {
	ruleProvisioner    *ruleProvisioner
	prometheusReloader *prometheusReloader
}

func (h *ruleProvisioningHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if r.Method != "PUT" {
		http.Error(w, "Bad request: only PUT requests supported", http.StatusBadRequest)
		return
	}

	reloadNecessary, err := h.ruleProvisioner.provision(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Internal Server Error: %v", err), http.StatusInternalServerError)
		return
	}

	if reloadNecessary {
		if err := h.prometheusReloader.trigger(ctx); err != nil {
			http.Error(w, fmt.Sprintf("Internal Server Error: %v", err), http.StatusInternalServerError)
			return
		}
	}
}

type ruleProvisioner struct {
	ruleFile string
}

// provision attempts to provision the rule files read from r, and if identical
// to existing, does not provision them. It returns whether Prometheus should
// be reloaded and if an error has occurred.
func (p *ruleProvisioner) provision(r io.Reader) (bool, error) {
	b := bytes.NewBuffer(nil)
	tr := io.TeeReader(r, b)

	f, err := os.Open(p.ruleFile)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if os.IsNotExist(err) {
		f, err = os.Create(p.ruleFile)
		if err != nil {
			return false, err
		}
	}

	equal, err := readersEqual(tr, f)
	if err != nil {
		return false, err
	}
	if equal {
		return false, nil
	}

	if err := f.Truncate(0); err != nil {
		return false, err
	}

	if _, err := io.Copy(f, b); err != nil {
		return false, err
	}

	return true, nil
}

func readersEqual(r1, r2 io.Reader) (bool, error) {
	buf1 := bufio.NewReader(r1)
	buf2 := bufio.NewReader(r2)
	for {
		b1, err1 := buf1.ReadByte()
		b2, err2 := buf2.ReadByte()
		if err1 != nil && err1 != io.EOF {
			return false, err1
		}
		if err2 != nil && err2 != io.EOF {
			return false, err2
		}
		if err1 == io.EOF || err2 == io.EOF {
			return err1 == err2, nil
		}
		if b1 != b2 {
			return false, nil
		}
	}
}

type prometheusReloader struct {
	prometheusReloadURL string
}

func (r *prometheusReloader) trigger(ctx context.Context) error {
	req, err := http.NewRequest("POST", r.prometheusReloadURL, nil)
	if err != nil {
		return errors.Wrap(err, "create request")
	}
	req = req.WithContext(ctx)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.Wrap(err, "reload request failed")
	}

	if _, err := io.Copy(ioutil.Discard, resp.Body); err != nil {
		return errors.Wrap(err, "exhausting request body failed")
	}

	if resp.StatusCode != 200 {
		return errors.Errorf("received non-200 response: %s; have you set `--web.enable-lifecycle` Prometheus flag?", resp.Status)
	}
	return nil
}
