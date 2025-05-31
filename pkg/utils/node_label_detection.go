/*
Copyright 2022 Adobe. All rights reserved.
This file is licensed to you under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License. You may obtain a copy
of the License at http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software distributed under
the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
OF ANY KIND, either express or implied. See the License for the specific language
governing permissions and limitations under the License.
*/

package utils

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/adobe/k8s-shredder/pkg/config"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// NodeLabelInfo holds information about a node that matches the label criteria
type NodeLabelInfo struct {
	Name   string
	Labels map[string]string
}

// parseLabelSelector parses a label selector string that can be either "key" or "key=value"
func parseLabelSelector(selector string) (string, string, bool) {
	if strings.Contains(selector, "=") {
		parts := strings.SplitN(selector, "=", 2)
		return parts[0], parts[1], true
	}
	return selector, "", false
}

// nodeMatchesLabelSelectors checks if a node matches any of the label selectors
func nodeMatchesLabelSelectors(node *v1.Node, labelSelectors []string) bool {
	nodeLabels := node.Labels
	if nodeLabels == nil {
		return false
	}

	for _, selector := range labelSelectors {
		key, value, hasValue := parseLabelSelector(selector)

		if nodeValue, exists := nodeLabels[key]; exists {
			if !hasValue {
				// If the selector is just a key, match if the key exists
				return true
			} else if nodeValue == value {
				// If the selector has a value, match if key=value
				return true
			}
		}
	}

	return false
}

// FindNodesWithLabels scans the kubernetes cluster for nodes that match the specified label selectors
// and excludes nodes that are already labeled as parked
func FindNodesWithLabels(ctx context.Context, k8sClient kubernetes.Interface, cfg config.Config) ([]NodeLabelInfo, error) {
	logger := log.WithField("function", "FindNodesWithLabels")

	if len(cfg.NodeLabelsToDetect) == 0 {
		logger.Debug("No node labels configured for detection")
		return []NodeLabelInfo{}, nil
	}

	logger.WithField("labelSelectors", cfg.NodeLabelsToDetect).Debug("Listing nodes with specified labels")

	// List all nodes
	nodeList, err := k8sClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "failed to list nodes")
	}

	var matchingNodes []NodeLabelInfo

	for _, node := range nodeList.Items {
		// Check if the node matches any of the label selectors
		if nodeMatchesLabelSelectors(&node, cfg.NodeLabelsToDetect) {
			// Check if the node is already labeled as parked
			isAlreadyParked, err := isNodeAlreadyParked(ctx, k8sClient, node.Name, cfg.UpgradeStatusLabel)
			if err != nil {
				logger.WithField("nodeName", node.Name).WithError(err).Warn("Failed to check if node is already parked, skipping")
				continue
			}

			if isAlreadyParked {
				logger.WithField("nodeName", node.Name).Debug("Node is already labeled as parked, skipping")
				continue
			}

			logger.WithField("nodeName", node.Name).Info("Found node matching label criteria")

			matchingNodes = append(matchingNodes, NodeLabelInfo{
				Name:   node.Name,
				Labels: node.Labels,
			})
		}
	}

	logger.WithField("matchingCount", len(matchingNodes)).Info("Found nodes matching label criteria")

	return matchingNodes, nil
}

// LabelNodesWithLabels labels nodes that match the configured label selectors with the standard parking labels
func LabelNodesWithLabels(ctx context.Context, k8sClient kubernetes.Interface, matchingNodes []NodeLabelInfo, cfg config.Config, dryRun bool) error {
	logger := log.WithField("function", "LabelNodesWithLabels")

	if len(matchingNodes) == 0 {
		logger.Debug("No matching nodes to label")
		return nil
	}

	// Calculate the expiration time
	expirationTime := time.Now().Add(cfg.DriftedNodeExpiration)
	expirationUnixTime := strconv.FormatInt(expirationTime.Unix(), 10)

	logger.WithFields(log.Fields{
		"upgradeStatusLabel": cfg.UpgradeStatusLabel,
		"expiresOnLabel":     cfg.ExpiresOnLabel,
		"expirationTime":     expirationTime.Format(time.RFC3339),
		"dryRun":             dryRun,
	}).Info("Starting to label nodes matching label criteria")

	for _, nodeInfo := range matchingNodes {
		// Label the node
		err := labelNode(ctx, k8sClient, nodeInfo.Name, cfg.UpgradeStatusLabel, "parked", cfg.ExpiresOnLabel, expirationUnixTime, cfg.ParkedByLabel, "k8s-shredder", dryRun)
		if err != nil {
			logger.WithField("nodeName", nodeInfo.Name).WithError(err).Error("Failed to label node")
			continue
		}

		// Cordon and taint the node
		err = cordonAndTaintNode(ctx, k8sClient, nodeInfo.Name, cfg.ParkedNodeTaint, dryRun)
		if err != nil {
			logger.WithField("nodeName", nodeInfo.Name).WithError(err).Error("Failed to cordon and taint node")
			// Continue with pod labeling even if cordoning/tainting fails
		}

		logger.WithField("nodeName", nodeInfo.Name).Info("Successfully processed node matching label criteria (labeled, cordoned, and tainted)")

		// Get eligible pods on the node and label them
		pods, err := getEligiblePodsForNode(ctx, k8sClient, nodeInfo.Name)
		if err != nil {
			logger.WithField("nodeName", nodeInfo.Name).WithError(err).Error("Failed to get pods for node")
			continue
		}

		logger.WithFields(log.Fields{
			"nodeName": nodeInfo.Name,
			"podCount": len(pods),
		}).Info("Found eligible pods to label on node matching label criteria")

		// Label each eligible pod
		for _, pod := range pods {
			err := labelPod(ctx, k8sClient, pod, cfg.UpgradeStatusLabel, "parked", cfg.ExpiresOnLabel, expirationUnixTime, cfg.ParkedByLabel, "k8s-shredder", dryRun)
			if err != nil {
				logger.WithFields(log.Fields{
					"pod":       pod.Name,
					"namespace": pod.Namespace,
					"nodeName":  nodeInfo.Name,
				}).WithError(err).Error("Failed to label pod")
				continue
			}

			logger.WithFields(log.Fields{
				"pod":       pod.Name,
				"namespace": pod.Namespace,
				"nodeName":  nodeInfo.Name,
			}).Debug("Successfully labeled pod on node matching label criteria")
		}
	}

	return nil
}

// ProcessNodesWithLabels is the main function that combines finding nodes with specific labels and parking them
func ProcessNodesWithLabels(ctx context.Context, appContext *AppContext) error {
	logger := log.WithField("function", "ProcessNodesWithLabels")

	logger.Info("Starting node label detection and parking process")

	// Find nodes with specified labels
	matchingNodes, err := FindNodesWithLabels(ctx, appContext.K8sClient, appContext.Config)
	if err != nil {
		return errors.Wrap(err, "failed to find nodes with specified labels")
	}

	if len(matchingNodes) == 0 {
		logger.Info("No nodes found matching the specified label criteria")
		return nil
	}

	// Label the nodes that match the criteria
	err = LabelNodesWithLabels(ctx, appContext.K8sClient, matchingNodes, appContext.Config, appContext.IsDryRun())
	if err != nil {
		return errors.Wrap(err, "failed to label nodes matching criteria")
	}

	logger.WithField("processedNodes", len(matchingNodes)).Info("Completed node label detection and parking process")

	return nil
}
