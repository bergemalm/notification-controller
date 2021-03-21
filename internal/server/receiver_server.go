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
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/sethvargo/go-limiter"
	"github.com/sethvargo/go-limiter/httplimit"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ReceiverServer handles webhook POST requests
type ReceiverServer struct {
	port       string
	logger     logr.Logger
	kubeClient client.Client
}

// NewEventServer returns an HTTP server that handles webhooks
func NewReceiverServer(port string, logger logr.Logger, kubeClient client.Client) *ReceiverServer {
	return &ReceiverServer{
		port:       port,
		logger:     logger.WithName("receiver-server"),
		kubeClient: kubeClient,
	}
}

// ListenAndServe starts the HTTP server on the specified port
func (s *ReceiverServer) ListenAndServe(stopCh <-chan struct{}, store limiter.Store) error {
	middleware, err := httplimit.NewMiddleware(store, receiverKeyFunc)
	if err != nil {
		return err
	}
	mux := http.DefaultServeMux
	mux.Handle("/hook/", middleware.Handle(http.HandlerFunc(s.handlePayload())))
	srv := &http.Server{
		Addr:    s.port,
		Handler: mux,
	}

	go func() {
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			s.logger.Error(err, "Receiver server crashed")
			os.Exit(1)
		}
	}()

	// wait for SIGTERM or SIGINT
	<-stopCh
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		s.logger.Error(err, "Receiver server graceful shutdown failed")
	} else {
		s.logger.Info("Receiver server stopped")
	}

	return nil
}

func receiverKeyFunc(r *http.Request) (string, error) {
	digest := url.PathEscape(strings.TrimLeft(r.RequestURI, "/hook/"))
	return fmt.Sprintf("receiver/%s", digest), nil
}
