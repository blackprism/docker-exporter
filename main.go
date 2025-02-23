package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/docker/docker/client"
	"github.com/google/gops/agent"
	"github.com/grafana/pyroscope-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/samber/oops"

	"github.com/blackprism/docker-exporter/cli"
)

func main() {
	go func() {
		err := agent.Listen(agent.Options{Addr: "0.0.0.0:50000"})
		if err != nil {
			return
		}
	}()

	serverAddress := os.Getenv("PYROSCOPE_SERVER_ADDRESS")

	if serverAddress != "" {
		pyroscope.Start(pyroscope.Config{
			ApplicationName: "docker-exporter",
			ServerAddress:   serverAddress,
			UploadRate:      1 * time.Second,
			ProfileTypes: []pyroscope.ProfileType{
				// these profile types are enabled by default:
				pyroscope.ProfileCPU,
				pyroscope.ProfileAllocObjects,
				pyroscope.ProfileAllocSpace,
				pyroscope.ProfileInuseObjects,
				pyroscope.ProfileInuseSpace,

				// these profile types are optional:
				pyroscope.ProfileGoroutines,
				pyroscope.ProfileMutexCount,
				pyroscope.ProfileMutexDuration,
				pyroscope.ProfileBlockCount,
				pyroscope.ProfileBlockDuration,
			},
		})
	}

	err := run(cli.MustGetParameter)

	if err != nil {
		slog.LogAttrs(context.Background(), slog.LevelError, "failed to start docker-exporter", slog.Any("error", err))
		os.Exit(1)
	}
}

func run(mustGetParameter func(string) any) error {
	port := mustGetParameter("port").(string)
	rootfs := mustGetParameter("rootfs").(string)
	volumeConcurrency := mustGetParameter("volume-concurrency").(int)
	volumeComputationLimit := mustGetParameter("volume-computation-limit").(int)
	dockerComposeOnly := mustGetParameter("docker-compose-only").(bool)
	volumeEnabled := mustGetParameter("volume-enabled").(bool)
	childProcessEnabled := mustGetParameter("child-process-enabled").(bool)

	cli, _ := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())

	r := prometheus.NewRegistry()
	r.MustRegister(
		newMetricProvider(cli, dockerComposeOnly, childProcessEnabled),
	)

	if volumeEnabled {
		r.MustRegister(
			newVolumeProvider(cli, dockerComposeOnly, rootfs, volumeConcurrency, int64(volumeComputationLimit)),
		)
	}

	http.HandleFunc("/metrics", func(w http.ResponseWriter, req *http.Request) {
		promhttp.HandlerFor(r, promhttp.HandlerOpts{ErrorHandling: promhttp.ContinueOnError}).ServeHTTP(w, req)
	})

	var addr strings.Builder
	addr.WriteString(":")
	addr.WriteString(port)

	errServer := http.ListenAndServe(addr.String(), nil)

	if errServer != nil {
		return oops.Wrapf(errServer, "failed to start http server")
	}

	return nil
}
