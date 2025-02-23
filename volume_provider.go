package main

import (
	"bytes"
	"context"
	"log/slog"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/errgroup"
)

type volumeMetric struct {
	name      string
	help      string
	valueType prometheus.ValueType
}

type volumeProvider struct {
	cli                    *client.Client
	dockerComposeOnly      bool
	rootfs                 string
	volumeConcurrency      int
	volumeComputationLimit int64
	volumeComputationUsage int64
	volumeLastCallWindow   time.Time
	metric                 volumeMetric
}

func newVolumeProvider(
	cli *client.Client,
	dockerComposeOnly bool,
	rootfs string,
	volumeConcurrency int,
	volumeComputationLimit int64,
) *volumeProvider {
	metric := volumeMetric{
		name:      "docker_volume_size_bytes",
		help:      "Size of a Docker volume in bytes.",
		valueType: prometheus.GaugeValue,
	}

	return &volumeProvider{
		cli:                    cli,
		dockerComposeOnly:      dockerComposeOnly,
		rootfs:                 rootfs,
		volumeConcurrency:      volumeConcurrency,
		volumeComputationLimit: volumeComputationLimit,
		metric:                 metric,
	}
}

func volumeDesc(m volumeMetric, labels []string) *prometheus.Desc {
	return prometheus.NewDesc(m.name, m.help, labels, nil)
}

func volumeLabels(labels map[string]string) ([]string, []string) {
	clabels := make([]string, 0, len(labels))
	cvalues := make([]string, 0, len(labels))

	for label, value := range labels {
		if value == "" {
			continue
		}
		clabels = append(clabels, label)
		cvalues = append(cvalues, value)
	}

	return clabels, cvalues
}

func (m volumeProvider) Describe(ch chan<- *prometheus.Desc) {
	ch <- volumeDesc(m.metric, nil)
}

func (m volumeProvider) Collect(ch chan<- prometheus.Metric) {
	if time.Since(m.volumeLastCallWindow) >= 1*time.Minute {
		m.volumeLastCallWindow = time.Now()
		m.volumeComputationUsage = int64(math.Max(0, float64(m.volumeComputationUsage-m.volumeComputationLimit)))
	}

	if m.volumeComputationUsage > m.volumeComputationLimit {
		return
	}

	start := time.Now()

	volumes, err := m.cli.VolumeList(context.Background(), volume.ListOptions{})

	if err != nil {
		slog.LogAttrs(context.Background(), slog.LevelError, "failed to list volumes", slog.Any("error", err))
		return
	}

	g, _ := errgroup.WithContext(context.Background())
	g.SetLimit(m.volumeConcurrency)

	for _, vol := range volumes.Volumes {
		if m.dockerComposeOnly && vol.Labels["com.docker.compose.project"] == "" {
			continue
		}

		g.Go(func() error {
			var path strings.Builder
			path.WriteString(m.rootfs)
			path.WriteString(vol.Mountpoint)
			command := []string{"du", "-bs", path.String()}
			cmd := exec.Command(command[0], command[1:]...)
			out, errCmd := cmd.Output()

			if errCmd != nil {
				slog.LogAttrs(context.Background(), slog.LevelError, "failed to get directory size", slog.Any("error", errCmd), slog.String("cmd", strings.Join(command, " ")))
				return nil
			}

			outSplit := bytes.SplitN(out, []byte("\t"), 2)

			if len(outSplit) != 2 {
				slog.LogAttrs(context.Background(), slog.LevelError, "unexpected output from du command", slog.String("output", string(out)))
				return nil
			}

			size, _ := strconv.Atoi(string(outSplit[0]))

			labels := make(map[string]string)
			labels["name"] = vol.Name
			labels["mountpoint"] = vol.Mountpoint
			labels["volume"] = vol.Labels["com.docker.compose.volume"]

			if vol.Labels["com.docker.compose.project"] != "" {
				labels["project"] = vol.Labels["com.docker.compose.project"]
			}

			clabels, cvalues := volumeLabels(labels)

			ch <- prometheus.MustNewConstMetric(
				volumeDesc(m.metric, clabels),
				m.metric.valueType,
				float64(size),
				cvalues...,
			)

			return nil
		})
	}

	err = g.Wait()

	m.volumeComputationUsage += time.Since(start).Milliseconds()

	if err != nil {
		return
	}
}
