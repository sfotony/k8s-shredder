/*
Copyright 2025 Adobe. All rights reserved.
This file is licensed to you under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License. You may obtain a copy
of the License at http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software distributed under
the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
OF ANY KIND, either express or implied. See the License for the specific language
governing permissions and limitations under the License.
*/

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (

	// ShredderAPIServerRequestsTotal = Total requests for Kubernetes API
	ShredderAPIServerRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "shredder_apiserver_requests_total",
			Help: "Total requests for Kubernetes API",
		},
		[]string{"verb", "resource", "status"},
	)

	// ShredderAPIServerRequestsDurationSeconds = Requests duration seconds for calling Kubernetes API
	ShredderAPIServerRequestsDurationSeconds = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name:       "shredder_apiserver_requests_duration_seconds",
			Help:       "Requests duration when calling Kubernetes API",
			Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
		},
		[]string{"verb", "resource", "status"},
	)

	// ShredderLoopsTotal = Total loops
	ShredderLoopsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "shredder_loops_total",
			Help: "Total loops",
		},
	)

	// ShredderLoopsDurationSeconds = Loops duration in seconds
	ShredderLoopsDurationSeconds = prometheus.NewSummary(
		prometheus.SummaryOpts{
			Name:       "shredder_loops_duration_seconds",
			Help:       "Loops duration in seconds",
			Objectives: map[float64]float64{0.5: 1200, 0.9: 900, 0.99: 600},
		},
	)

	// ShredderProcessedNodesTotal = Total processed nodes
	ShredderProcessedNodesTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "shredder_processed_nodes_total",
			Help: "Total processed nodes",
		},
	)

	// ShredderProcessedPodsTotal = Total processed pods
	ShredderProcessedPodsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "shredder_processed_pods_total",
			Help: "Total processed pods",
		},
	)

	// ShredderErrorsTotal = Total errors
	ShredderErrorsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "shredder_errors_total",
			Help: "Total errors",
		},
	)

	// ShredderPodErrorsTotal = Total pod errors
	ShredderPodErrorsTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "shredder_pod_errors_total",
			Help: "Total pod errors per eviction loop",
		},
		[]string{"pod_name", "namespace", "reason", "action"},
	)

	// ShredderNodeForceToEvictTime = Time when the node will be forcibly evicted
	ShredderNodeForceToEvictTime = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "shredder_node_force_to_evict_time",
			Help: "Time when the node will be forcibly evicted",
		},
		[]string{"node_name"},
	)

	// ShredderPodForceToEvictTime = Time when the pod will be forcibly evicted
	ShredderPodForceToEvictTime = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "shredder_pod_force_to_evict_time",
			Help: "Time when the pod will be forcibly evicted",
		},
		[]string{"pod_name", "namespace"},
	)

	// ShredderKarpenterDriftedNodesTotal = Total number of drifted Karpenter nodes detected
	ShredderKarpenterDriftedNodesTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "shredder_karpenter_drifted_nodes_total",
			Help: "Total number of drifted Karpenter nodes detected",
		},
	)

	// ShredderKarpenterNodesParkedTotal = Total number of Karpenter nodes successfully parked
	ShredderKarpenterNodesParkedTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "shredder_karpenter_nodes_parked_total",
			Help: "Total number of Karpenter nodes successfully parked",
		},
	)

	// ShredderKarpenterNodesParkingFailedTotal = Total number of Karpenter nodes that failed to be parked
	ShredderKarpenterNodesParkingFailedTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "shredder_karpenter_nodes_parking_failed_total",
			Help: "Total number of Karpenter nodes that failed to be parked",
		},
	)

	// ShredderKarpenterProcessingDurationSeconds = Duration of Karpenter node processing in seconds
	ShredderKarpenterProcessingDurationSeconds = prometheus.NewSummary(
		prometheus.SummaryOpts{
			Name:       "shredder_karpenter_processing_duration_seconds",
			Help:       "Duration of Karpenter node processing in seconds",
			Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
		},
	)

	// ShredderNodeLabelNodesParkedTotal = Total number of nodes successfully parked via node label detection
	ShredderNodeLabelNodesParkedTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "shredder_node_label_nodes_parked_total",
			Help: "Total number of nodes successfully parked via node label detection",
		},
	)

	// ShredderNodeLabelNodesParkingFailedTotal = Total number of nodes that failed to be parked via node label detection
	ShredderNodeLabelNodesParkingFailedTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "shredder_node_label_nodes_parking_failed_total",
			Help: "Total number of nodes that failed to be parked via node label detection",
		},
	)

	// ShredderNodeLabelProcessingDurationSeconds = Duration of node label detection and parking process in seconds
	ShredderNodeLabelProcessingDurationSeconds = prometheus.NewSummary(
		prometheus.SummaryOpts{
			Name:       "shredder_node_label_processing_duration_seconds",
			Help:       "Duration of node label detection and parking process in seconds",
			Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
		},
	)

	// ShredderNodeLabelMatchingNodesTotal = Total number of nodes matching the label criteria
	ShredderNodeLabelMatchingNodesTotal = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "shredder_node_label_matching_nodes_total",
			Help: "Total number of nodes matching the label criteria",
		},
	)

	// ShredderNodesParkedTotal = Total number of nodes successfully parked (shared across all detection methods)
	ShredderNodesParkedTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "shredder_nodes_parked_total",
			Help: "Total number of nodes successfully parked (shared across all detection methods)",
		},
	)

	// ShredderNodesParkingFailedTotal = Total number of nodes that failed to be parked (shared across all detection methods)
	ShredderNodesParkingFailedTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "shredder_nodes_parking_failed_total",
			Help: "Total number of nodes that failed to be parked (shared across all detection methods)",
		},
	)

	// ShredderProcessingDurationSeconds = Duration of node processing in seconds (shared across all detection methods)
	ShredderProcessingDurationSeconds = prometheus.NewSummary(
		prometheus.SummaryOpts{
			Name:       "shredder_processing_duration_seconds",
			Help:       "Duration of node processing in seconds (shared across all detection methods)",
			Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
		},
	)
)
