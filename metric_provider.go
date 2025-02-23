package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/prometheus/client_golang/prometheus"
)

type dockerMetric struct {
	name      string
	help      string
	valueType prometheus.ValueType
	getValues func(cont types.Container, containerStats container.StatsResponse) []metricValue
}

type metricValue struct {
	value     float64
	timestamp time.Time
	labels    map[string]string
}

type metricProvider struct {
	cli               *client.Client
	dockerComposeOnly bool
	metrics           []dockerMetric
}

func newMetricProvider(cli *client.Client, dockerComposeOnly bool, childProcessEnabled bool) *metricProvider {
	metrics := []dockerMetric{
		{
			name:      "docker_cpu_user_seconds_total",
			help:      "Cumulative user cpu time consumed in seconds.",
			valueType: prometheus.CounterValue,
			getValues: func(cont types.Container, containerStats container.StatsResponse) []metricValue {
				labels := make(map[string]string)
				labels["id"] = cont.ID
				labels["name"] = strings.TrimPrefix(cont.Names[0], "/")

				if cont.Labels["com.docker.compose.project"] != "" {
					labels["project"] = cont.Labels["com.docker.compose.project"]
				}

				if cont.Labels["com.docker.compose.service"] != "" {
					labels["service"] = cont.Labels["com.docker.compose.service"]
				}

				return []metricValue{
					{
						value:     float64(containerStats.CPUStats.CPUUsage.UsageInUsermode) / 1_000_000_000.0,
						timestamp: time.Now(),
						labels:    labels,
					},
				}
			},
		},
		{
			name:      "docker_cpu_system_seconds_total",
			help:      "Cumulative system cpu time consumed in seconds.",
			valueType: prometheus.CounterValue,
			getValues: func(cont types.Container, containerStats container.StatsResponse) []metricValue {
				labels := make(map[string]string)
				labels["id"] = cont.ID
				labels["name"] = strings.TrimPrefix(cont.Names[0], "/")

				if cont.Labels["com.docker.compose.project"] != "" {
					labels["project"] = cont.Labels["com.docker.compose.project"]
				}

				if cont.Labels["com.docker.compose.service"] != "" {
					labels["service"] = cont.Labels["com.docker.compose.service"]
				}

				return []metricValue{
					{
						value:     float64(containerStats.CPUStats.CPUUsage.UsageInKernelmode) / 1_000_000_000.0,
						timestamp: time.Now(),
						labels:    labels,
					},
				}
			},
		},
		{
			name:      "docker_memory_usage_bytes",
			help:      "Current memory usage in bytes, including all memory regardless of when it was accessed.",
			valueType: prometheus.GaugeValue,
			getValues: func(cont types.Container, containerStats container.StatsResponse) []metricValue {
				labels := make(map[string]string)
				labels["id"] = cont.ID
				labels["name"] = strings.TrimPrefix(cont.Names[0], "/")

				if cont.Labels["com.docker.compose.project"] != "" {
					labels["project"] = cont.Labels["com.docker.compose.project"]
				}

				if cont.Labels["com.docker.compose.service"] != "" {
					labels["service"] = cont.Labels["com.docker.compose.service"]
				}

				return []metricValue{
					{
						value:     float64(containerStats.MemoryStats.Usage),
						timestamp: time.Now(),
						labels:    labels,
					},
				}
			},
		},
		{
			name:      "docker_memory_working_set_bytes",
			help:      "Current working set in bytes.",
			valueType: prometheus.GaugeValue,
			getValues: func(cont types.Container, containerStats container.StatsResponse) []metricValue {
				labels := make(map[string]string)
				labels["id"] = cont.ID
				labels["name"] = strings.TrimPrefix(cont.Names[0], "/")

				if cont.Labels["com.docker.compose.project"] != "" {
					labels["project"] = cont.Labels["com.docker.compose.project"]
				}

				if cont.Labels["com.docker.compose.service"] != "" {
					labels["service"] = cont.Labels["com.docker.compose.service"]
				}

				workingSet := containerStats.MemoryStats.Usage
				inactiveFile := containerStats.MemoryStats.Stats["inactive_file"]

				if workingSet < inactiveFile {
					workingSet = 0
				} else {
					workingSet -= inactiveFile
				}

				return []metricValue{
					{
						value:     float64(workingSet),
						timestamp: time.Now(),
						labels:    labels,
					},
				}
			},
		},
		{
			name:      "docker_fs_reads_bytes_total",
			help:      "Cumulative count of bytes read.",
			valueType: prometheus.CounterValue,
			getValues: func(cont types.Container, containerStats container.StatsResponse) []metricValue {
				labels := make(map[string]string)
				labels["id"] = cont.ID
				labels["name"] = strings.TrimPrefix(cont.Names[0], "/")

				if cont.Labels["com.docker.compose.project"] != "" {
					labels["project"] = cont.Labels["com.docker.compose.project"]
				}

				if cont.Labels["com.docker.compose.service"] != "" {
					labels["service"] = cont.Labels["com.docker.compose.service"]
				}

				read := 0
				for _, ioStatEntry := range containerStats.BlkioStats.IoServiceBytesRecursive {
					if ioStatEntry.Op != "read" {
						continue
					}

					read += int(ioStatEntry.Value)
				}

				return []metricValue{
					{
						value:     float64(read),
						timestamp: time.Now(),
						labels:    labels,
					},
				}
			},
		},
		{
			name:      "docker_fs_writes_bytes_total",
			help:      "Cumulative count of bytes written.",
			valueType: prometheus.CounterValue,
			getValues: func(cont types.Container, containerStats container.StatsResponse) []metricValue {
				labels := make(map[string]string)
				labels["id"] = cont.ID
				labels["name"] = strings.TrimPrefix(cont.Names[0], "/")

				if cont.Labels["com.docker.compose.project"] != "" {
					labels["project"] = cont.Labels["com.docker.compose.project"]
				}

				if cont.Labels["com.docker.compose.service"] != "" {
					labels["service"] = cont.Labels["com.docker.compose.service"]
				}

				write := 0
				for _, ioStatEntry := range containerStats.BlkioStats.IoServiceBytesRecursive {
					if ioStatEntry.Op != "write" {
						continue
					}

					write += int(ioStatEntry.Value)
				}

				return []metricValue{
					{
						value:     float64(write),
						timestamp: time.Now(),
						labels:    labels,
					},
				}
			},
		},
		{
			name:      "docker_network_receive_bytes_total",
			help:      "Cumulative count of bytes received.",
			valueType: prometheus.CounterValue,
			getValues: func(cont types.Container, containerStats container.StatsResponse) []metricValue {
				labels := make(map[string]string)
				labels["id"] = cont.ID
				labels["name"] = strings.TrimPrefix(cont.Names[0], "/")

				if cont.Labels["com.docker.compose.project"] != "" {
					labels["project"] = cont.Labels["com.docker.compose.project"]
				}

				if cont.Labels["com.docker.compose.service"] != "" {
					labels["service"] = cont.Labels["com.docker.compose.service"]
				}

				var metricValues []metricValue

				for iface, networkStats := range containerStats.Networks {
					labelsIface := make(map[string]string)
					for k, v := range labels {
						labelsIface[k] = v
					}
					labelsIface["interface"] = iface
					metricValues = append(metricValues, metricValue{
						value:     float64(networkStats.RxBytes),
						timestamp: time.Now(),
						labels:    labelsIface,
					})
				}

				return metricValues
			},
		},
		{
			name:      "docker_network_transmit_bytes_total",
			help:      "Cumulative count of bytes transmitted.",
			valueType: prometheus.CounterValue,
			getValues: func(cont types.Container, containerStats container.StatsResponse) []metricValue {
				labels := make(map[string]string)
				labels["id"] = cont.ID
				labels["name"] = strings.TrimPrefix(cont.Names[0], "/")

				if cont.Labels["com.docker.compose.project"] != "" {
					labels["project"] = cont.Labels["com.docker.compose.project"]
				}

				if cont.Labels["com.docker.compose.service"] != "" {
					labels["service"] = cont.Labels["com.docker.compose.service"]
				}

				var metricValues []metricValue

				for iface, networkStats := range containerStats.Networks {
					labelsIface := make(map[string]string)
					for k, v := range labels {
						labelsIface[k] = v
					}
					labelsIface["interface"] = iface
					metricValues = append(metricValues, metricValue{
						value:     float64(networkStats.TxBytes),
						timestamp: time.Now(),
						labels:    labelsIface,
					})
				}

				return metricValues
			},
		},
	}

	if childProcessEnabled {
		metrics = append(metrics, dockerMetric{
			name:      "docker_child_process_cpu_percent_instant",
			help:      "Instant child process cpu time in percent.",
			valueType: prometheus.GaugeValue,
			getValues: func(cont types.Container, containerStats container.StatsResponse) []metricValue {
				labels := make(map[string]string)
				labels["id"] = cont.ID
				labels["name"] = strings.TrimPrefix(cont.Names[0], "/")

				if cont.Labels["com.docker.compose.project"] != "" {
					labels["project"] = cont.Labels["com.docker.compose.project"]
				}

				if cont.Labels["com.docker.compose.service"] != "" {
					labels["service"] = cont.Labels["com.docker.compose.service"]
				}

				processList, _ := cli.ContainerTop(context.Background(), cont.ID, []string{"aux"})

				statIndex := -1
				cpuIndex := -1
				commandIndex := -1
				for index, name := range processList.Titles {
					if name == "STAT" {
						statIndex = index
						continue
					}

					if name == "%CPU" {
						cpuIndex = index
						continue
					}

					if name == "COMMAND" {
						commandIndex = index
						continue
					}
				}

				if cpuIndex == -1 || statIndex == -1 || commandIndex == -1 {
					return []metricValue{}
				}

				var metricValues []metricValue

				for _, process := range processList.Processes {
					value, _ := strconv.ParseFloat(process[cpuIndex], 64)

					labelsCommand := make(map[string]string)
					for k, v := range labels {
						labelsCommand[k] = v
					}

					processType := "child"

					if strings.Contains(process[statIndex], "s") {
						processType = "main"
					}

					reg := regexp.MustCompile(`[^\w-.]`)
					name := strings.Split(process[commandIndex], " ")[0]
					name = filepath.Base(name)
					name = reg.ReplaceAllString(name, " ")

					labelsCommand["name"] = name
					labelsCommand["process_type"] = processType
					labelsCommand["command"] = process[commandIndex]

					metricValues = append(metricValues, metricValue{
						value:     value,
						timestamp: time.Time{},
						labels:    labelsCommand,
					})
				}

				return metricValues
			},
		},
		)
	}

	return &metricProvider{
		cli:               cli,
		dockerComposeOnly: dockerComposeOnly,
		metrics:           metrics,
	}
}

func metricDesc(m dockerMetric, labels []string) *prometheus.Desc {
	return prometheus.NewDesc(m.name, m.help, labels, nil)
}

func metricLabels(labels map[string]string) ([]string, []string) {
	clabels := make([]string, 0, len(labels))
	cvalues := make([]string, 0, len(labels))

	for label, value := range labels {
		clabels = append(clabels, label)
		cvalues = append(cvalues, value)
	}

	return clabels, cvalues
}

func (m metricProvider) Describe(ch chan<- *prometheus.Desc) {
	for _, metric := range m.metrics {
		ch <- metricDesc(metric, nil)
	}
}

func (m metricProvider) Collect(ch chan<- prometheus.Metric) {
	containers, errContainer := m.cli.ContainerList(context.Background(), container.ListOptions{})

	if errContainer != nil {
		slog.LogAttrs(context.Background(), slog.LevelError, "failed to list container", slog.Any("error", errContainer))
		return
	}

	for _, cont := range containers {
		if m.dockerComposeOnly && cont.Labels["com.docker.compose.project"] == "" {
			continue
		}

		containerStats, errContainerStats := m.getContainerStats(cont)

		if errContainerStats != nil {
			continue
		}

		for _, metric := range m.metrics {
			metricValues := metric.getValues(cont, containerStats)

			for _, metricValue := range metricValues {
				clabels, cvalues := metricLabels(metricValue.labels)

				ch <- prometheus.MustNewConstMetric(
					metricDesc(metric, clabels),
					metric.valueType,
					metricValue.value,
					cvalues...,
				)
			}
		}
	}
}

func (m metricProvider) getContainerStats(cont types.Container) (container.StatsResponse, error) {
	statsResponseHeader, errStats := m.cli.ContainerStatsOneShot(context.Background(), cont.ID)

	if errStats != nil {
		slog.LogAttrs(context.Background(), slog.LevelError, "failed to get container stats", slog.Any("error", errStats))
		return container.StatsResponse{}, errStats
	}

	defer statsResponseHeader.Body.Close()

	var containerStats container.StatsResponse
	errJson := json.NewDecoder(statsResponseHeader.Body).Decode(&containerStats)

	if errJson != nil {
		slog.LogAttrs(context.Background(), slog.LevelError, "failed to decode container stats", slog.Any("error", errJson), slog.Any("containerStats", containerStats))
		return container.StatsResponse{}, errJson
	}

	return containerStats, nil
}
