/*
Copyright 2020 The Flux authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"time"

	"github.com/go-logr/logr"
	"github.com/sethvargo/go-limiter"
	"github.com/sethvargo/go-limiter/httplimit"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/fluxcd/pkg/recorder"
)

// EventServer handles event POST requests
type EventServer struct {
	port       string
	logger     logr.Logger
	kubeClient client.Client
}

// NewEventServer returns an HTTP server that handles events
func NewEventServer(port string, logger logr.Logger, kubeClient client.Client) *EventServer {
	return &EventServer{
		port:       port,
		logger:     logger.WithName("event-server"),
		kubeClient: kubeClient,
	}
}

// ListenAndServe starts the HTTP server on the specified port
func (s *EventServer) ListenAndServe(stopCh <-chan struct{}, store limiter.Store) error {
	middleware, err := httplimit.NewMiddleware(store, eventKeyFunc)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.Handle("/", middleware.Handle(http.HandlerFunc(s.handleEvent())))
	srv := &http.Server{
		Addr:    s.port,
		Handler: mux,
	}

	go func() {
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			s.logger.Error(err, "Event server crashed")
			os.Exit(1)
		}
	}()

	// wait for SIGTERM or SIGINT
	<-stopCh
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		s.logger.Error(err, "Event server graceful shutdown failed")
	} else {
		s.logger.Info("Event server stopped")
	}

	return nil
}

func eventKeyFunc(r *http.Request) (string, error) {
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return "", err
	}
	defer r.Body.Close()

	event := &recorder.Event{}
	err = json.Unmarshal(body, event)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("event/%s/%s", event.InvolvedObject.String(), event.Severity), nil
}
