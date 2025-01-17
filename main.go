package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/google/gops/agent"
	pyroscope "github.com/grafana/pyroscope-go"
	"github.com/samber/oops"
)

var defaultPort = "9100"
var defaultRootFSDirectory = "/rootfs"
var defaultMetricConcurrency = 100
var defaultVolumeConcurrency = 10
var defaultVolumeComputationLimit = 10000

func main() {
	ctx := context.Background()

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
			Logger:          pyroscope.StandardLogger,
		})
	}

	err := run(ctx, os.Getenv)

	if err != nil {
		slog.LogAttrs(context.Background(), slog.LevelError, "failed to start docker-exporter", slog.Any("error", err))
		os.Exit(1)
	}
}

func run(ctx context.Context, getenv func(string) string) error {
	rootfs := getenv("ROOTFS_DIRECTORY")

	if rootfs == "" {
		rootfs = defaultRootFSDirectory
	}

	metricConcurrency, err := strconv.Atoi(getenv("METRIC_CONCURRENCY"))

	if metricConcurrency == 0 || err != nil {
		metricConcurrency = defaultMetricConcurrency
	}

	volumeConcurrency, err := strconv.Atoi(getenv("VOLUME_CONCURRENCY"))

	if volumeConcurrency == 0 || err != nil {
		volumeConcurrency = defaultVolumeConcurrency
	}

	volumeComputationLimit, err := strconv.Atoi(getenv("VOLUME_COMPUTATION_LIMIT"))

	if volumeComputationLimit == 0 || err != nil {
		volumeComputationLimit = defaultVolumeComputationLimit
	}

	m := Metrics{
		RootFS:                 rootfs,
		MetricConcurrency:      metricConcurrency,
		VolumeConcurrency:      volumeConcurrency,
		VolumeComputationLimit: int64(volumeComputationLimit),
	}

	http.HandleFunc("/metrics", m.Metrics)

	port := getenv("PORT")

	if port == "" {
		port = defaultPort
	}

	var addr strings.Builder
	addr.WriteString(":")
	addr.WriteString(port)

	errServer := http.ListenAndServe(addr.String(), nil)

	if errServer != nil {
		return oops.Wrapf(errServer, "failed to start http server")
	}

	return nil
}
