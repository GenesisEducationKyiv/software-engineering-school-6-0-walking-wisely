// Package main contains server startup helpers for registering routes on the gRPC-Gateway mux.
package main

import (
	"log"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
)

func MustHandlePath(mux *runtime.ServeMux, method, path string, cb runtime.HandlerFunc) {
	if err := mux.HandlePath(method, path, cb); err != nil {
		log.Fatalf("failed to register route %s %s: %v", method, path, err)
	}
}
