package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"golang.org/x/sync/errgroup"
)

type Metrics struct {
	RootFS                 string
	MetricConcurrency      int
	VolumeConcurrency      int
	VolumeComputationLimit int64
	volumeComputationUsage int64
	volumeLastCallWindow   time.Time
}

func (m *Metrics) Metrics(w http.ResponseWriter, r *http.Request) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		slog.LogAttrs(context.Background(), slog.LevelError, "error getting docker client", slog.Any("error", err))
	}

	g, _ := errgroup.WithContext(r.Context())

	if r.URL.Query().Has("cpu") {
		g.Go(func() error {
			m.metricCPU(cli, w, r, r.URL.Query().Has("only_project"))
			return nil
		})
	}

	if r.URL.Query().Has("volume") {
		g.Go(func() error {
			m.metricVolume(cli, w, r, r.URL.Query().Has("only_project"))
			return nil
		})
	}
	g.Wait()
}

func (m *Metrics) metricCPU(cli *client.Client, w http.ResponseWriter, r *http.Request, onlyProject bool) {
	containers, errContainer := cli.ContainerList(r.Context(), container.ListOptions{})

	if errContainer != nil {
		slog.LogAttrs(context.Background(), slog.LevelError, "failed to list container", slog.Any("error", errContainer))
		return
	}

	metrics := make(chan string, len(containers)*2)

	g, _ := errgroup.WithContext(r.Context())
	g.SetLimit(m.MetricConcurrency)

	for _, cont := range containers {
		if onlyProject && cont.Labels["com.docker.compose.project"] == "" {
			continue
		}

		g.Go(func() error {
			statsResponseHeader, errStats := cli.ContainerStatsOneShot(r.Context(), cont.ID)

			if errStats != nil {
				slog.LogAttrs(context.Background(), slog.LevelError, "failed to get container stats", slog.Any("error", errStats))
				return nil
			}

			defer statsResponseHeader.Body.Close()

			var containerStats container.StatsResponse
			errJson := json.NewDecoder(statsResponseHeader.Body).Decode(&containerStats)

			if errJson != nil {
				slog.LogAttrs(context.Background(), slog.LevelError, "failed to decode container stats", slog.Any("error", errJson), slog.Any("containerStats", containerStats))
				return nil
			}

			metrics <- buildCPUMetric(cont, containerStats, "docker_cpu_user_seconds_total", strconv.FormatFloat(float64(containerStats.CPUStats.CPUUsage.UsageInUsermode)/1_000_000_000.0, 'f', -1, 64))
			metrics <- buildCPUMetric(cont, containerStats, "docker_cpu_system_seconds_total", strconv.FormatFloat(float64(containerStats.CPUStats.CPUUsage.UsageInKernelmode)/1_000_000_000.0, 'f', -1, 64))

			return nil
		})
	}

	err := g.Wait()

	if err != nil {
		return
	}

	close(metrics)

	for metric := range metrics {
		_, errWrite := io.WriteString(w, metric)
		if errWrite != nil {
			slog.LogAttrs(context.Background(), slog.LevelError, "failed to write metric", slog.Any("error", errWrite))
			return
		}
	}
}

func buildCPUMetric(cont types.Container, containerStats container.StatsResponse, name string, value string) string {
	var metric strings.Builder
	metric.WriteString(name)
	metric.WriteString("{id=")
	metric.WriteString(strconv.Quote(cont.ID))
	metric.WriteString(",name=")
	metric.WriteString(strconv.Quote(strings.TrimPrefix(cont.Names[0], "/")))

	if cont.Labels["com.docker.compose.project"] != "" {
		metric.WriteString(",project=")
		metric.WriteString(strconv.Quote(cont.Labels["com.docker.compose.project"]))
	}

	metric.WriteString("} ")
	metric.WriteString(value)
	metric.WriteString("\n")

	return metric.String()
}

func (m *Metrics) metricVolume(cli *client.Client, w http.ResponseWriter, r *http.Request, onlyProject bool) {
	if time.Since(m.volumeLastCallWindow) >= 1*time.Minute {
		m.volumeLastCallWindow = time.Now()
		m.volumeComputationUsage = int64(math.Max(0, float64(m.volumeComputationUsage-m.VolumeComputationLimit)))
	}

	if m.volumeComputationUsage > m.VolumeComputationLimit {
		return
	}

	start := time.Now()

	volumes, err := cli.VolumeList(context.Background(), volume.ListOptions{})
	if err != nil {
		slog.LogAttrs(context.Background(), slog.LevelError, "failed to list volumes", slog.Any("error", err))
		return
	}

	metrics := make(chan string, len(volumes.Volumes))

	g, _ := errgroup.WithContext(r.Context())
	g.SetLimit(m.VolumeConcurrency)

	for _, vol := range volumes.Volumes {
		if onlyProject && vol.Labels["com.docker.compose.project"] == "" {
			continue
		}

		g.Go(func() error {
			var path strings.Builder
			path.WriteString(m.RootFS)
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

			metrics <- buildVolumeMetric(vol, outSplit[0])

			return nil
		})
	}

	err = g.Wait()

	if err != nil {
		return
	}

	close(metrics)

	for metric := range metrics {
		_, errWrite := io.WriteString(w, metric)
		if errWrite != nil {
			slog.LogAttrs(context.Background(), slog.LevelError, "failed to write metric", slog.Any("error", errWrite))
			return
		}
	}

	m.volumeComputationUsage += time.Since(start).Milliseconds()
}

func buildVolumeMetric(vol *volume.Volume, size []byte) string {
	var metric strings.Builder
	metric.WriteString("docker_volume_size_bytes{name=")
	metric.WriteString(strconv.Quote(vol.Name))
	metric.WriteString(",mountpoint=")
	metric.WriteString(strconv.Quote(vol.Mountpoint))
	if vol.Labels["com.docker.compose.project"] != "" {
		metric.WriteString(",project=")
		metric.WriteString(strconv.Quote(vol.Labels["com.docker.compose.project"]))
	}
	if vol.Labels["com.docker.compose.volume"] != "" {
		metric.WriteString(",volume=")
		metric.WriteString(strconv.Quote(vol.Labels["com.docker.compose.volume"]))
	}
	metric.WriteString("} ")
	metric.Write(size)
	metric.WriteString("\n")

	return metric.String()
}
