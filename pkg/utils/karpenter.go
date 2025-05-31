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
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/adobe/k8s-shredder/pkg/config"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"golang.org/x/exp/slices"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

const (
	// Karpenter API constants
	KarpenterAPIGroup   = "karpenter.sh"
	KarpenterAPIVersion = "v1"
	NodeClaimResource   = "nodeclaims"

	// Karpenter condition types
	KarpenterDriftedCondition = "Drifted"
	KarpenterTrueStatus       = "True"
)

// KarpenterNodeClaimInfo holds information about a Karpenter NodeClaim
type KarpenterNodeClaimInfo struct {
	Name       string
	Namespace  string
	NodeName   string
	ProviderID string
	IsDrifted  bool
}

// FindDriftedKarpenterNodeClaims scans the kubernetes cluster for Karpenter NodeClaims that are marked as drifted
// and excludes nodes that are already labeled as parked
func FindDriftedKarpenterNodeClaims(ctx context.Context, dynamicClient dynamic.Interface, k8sClient kubernetes.Interface, cfg config.Config) ([]KarpenterNodeClaimInfo, error) {
	logger := log.WithField("function", "FindDriftedKarpenterNodeClaims")

	// Create a GVR for Karpenter NodeClaims
	gvr := schema.GroupVersionResource{
		Group:    KarpenterAPIGroup,
		Version:  KarpenterAPIVersion,
		Resource: NodeClaimResource,
	}

	logger.Debug("Listing Karpenter NodeClaims")

	// List all NodeClaims
	nodeClaimList, err := dynamicClient.Resource(gvr).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "failed to list Karpenter NodeClaims")
	}

	var driftedNodeClaims []KarpenterNodeClaimInfo

	for _, item := range nodeClaimList.Items {
		nodeClaim := item.Object

		// Extract NodeClaim name and namespace
		name, _, err := unstructured.NestedString(nodeClaim, "metadata", "name")
		if err != nil || name == "" {
			logger.WithField("nodeclaim", item.GetName()).Warn("Failed to get NodeClaim name")
			continue
		}

		namespace, _, err := unstructured.NestedString(nodeClaim, "metadata", "namespace")
		if err != nil {
			namespace = "default" // NodeClaims might be cluster-scoped
		}

		// Check if the NodeClaim is drifted by examining its conditions
		isDrifted, err := isNodeClaimDrifted(nodeClaim)
		if err != nil {
			logger.WithField("nodeclaim", name).WithError(err).Warn("Failed to check drift status")
			continue
		}

		if isDrifted {
			// Get the associated node information
			nodeName, providerID := getNodeInfoFromNodeClaim(nodeClaim)

			// Skip if no node is associated
			if nodeName == "" {
				logger.WithField("nodeclaim", name).Debug("NodeClaim has no associated node, skipping")
				continue
			}

			// Check if the node is already labeled as parked
			isAlreadyParked, err := isNodeAlreadyParked(ctx, k8sClient, nodeName, cfg.UpgradeStatusLabel)
			if err != nil {
				logger.WithFields(log.Fields{
					"nodeclaim": name,
					"nodeName":  nodeName,
				}).WithError(err).Warn("Failed to check if node is already parked, skipping")
				continue
			}

			if isAlreadyParked {
				logger.WithFields(log.Fields{
					"nodeclaim": name,
					"nodeName":  nodeName,
				}).Debug("Node is already labeled as parked, skipping")
				continue
			}

			logger.WithFields(log.Fields{
				"nodeclaim": name,
				"nodeName":  nodeName,
				"namespace": namespace,
			}).Info("Found drifted NodeClaim with unlabeled node")

			driftedNodeClaims = append(driftedNodeClaims, KarpenterNodeClaimInfo{
				Name:       name,
				Namespace:  namespace,
				NodeName:   nodeName,
				ProviderID: providerID,
				IsDrifted:  true,
			})
		}
	}

	logger.WithField("driftedCount", len(driftedNodeClaims)).Info("Found drifted Karpenter NodeClaims")

	return driftedNodeClaims, nil
}

// isNodeClaimDrifted checks if a NodeClaim has the "Drifted" condition set to "True"
func isNodeClaimDrifted(nodeClaim map[string]interface{}) (bool, error) {
	conditions, found, err := unstructured.NestedSlice(nodeClaim, "status", "conditions")
	if err != nil {
		return false, errors.Wrap(err, "failed to get conditions from NodeClaim")
	}

	if !found {
		return false, nil // No conditions means not drifted
	}

	for _, conditionInterface := range conditions {
		condition, ok := conditionInterface.(map[string]interface{})
		if !ok {
			continue
		}

		conditionType, _, err := unstructured.NestedString(condition, "type")
		if err != nil {
			continue
		}

		if conditionType == KarpenterDriftedCondition {
			status, _, err := unstructured.NestedString(condition, "status")
			if err != nil {
				continue
			}

			return status == KarpenterTrueStatus, nil
		}
	}

	return false, nil
}

// getNodeInfoFromNodeClaim extracts node name and provider ID from a NodeClaim
func getNodeInfoFromNodeClaim(nodeClaim map[string]interface{}) (string, string) {
	nodeName, _, _ := unstructured.NestedString(nodeClaim, "status", "nodeName")
	providerID, _, _ := unstructured.NestedString(nodeClaim, "status", "providerID")

	return nodeName, providerID
}

// isNodeAlreadyParked checks if a node is already labeled with the parked status
func isNodeAlreadyParked(ctx context.Context, k8sClient kubernetes.Interface, nodeName, upgradeStatusLabel string) (bool, error) {
	node, err := k8sClient.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return false, errors.Wrapf(err, "failed to get node %s", nodeName)
	}

	if node.Labels == nil {
		return false, nil
	}

	upgradeStatus, exists := node.Labels[upgradeStatusLabel]
	return exists && upgradeStatus == "parked", nil
}

// getEligiblePodsForNode returns all eligible for labeling pods from a specific node (excluding DaemonSet and static pods)
func getEligiblePodsForNode(ctx context.Context, k8sClient kubernetes.Interface, nodeName string) ([]v1.Pod, error) {
	podList, err := k8sClient.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("spec.nodeName=%s", nodeName),
	})

	if err != nil {
		return nil, err
	}

	var podListCleaned []v1.Pod

	// we need to remove any non-eligible pods
	for _, pod := range podList.Items {
		// skip pods in terminating state
		if pod.DeletionTimestamp != nil {
			continue
		}

		// skip pods with DaemonSet controller object or static pods
		if len(pod.OwnerReferences) > 0 && slices.Contains([]string{"DaemonSet", "Node"}, pod.ObjectMeta.OwnerReferences[0].Kind) {
			continue
		}

		podListCleaned = append(podListCleaned, pod)
	}

	return podListCleaned, nil
}

// parseTaintString parses a taint string in the format key=value:effect and returns the components
func parseTaintString(taintStr string) (string, string, v1.TaintEffect, error) {
	// Split by colon to separate key=value from effect
	parts := strings.Split(taintStr, ":")
	if len(parts) != 2 {
		return "", "", "", errors.Errorf("invalid taint format, expected key=value:effect, got %s", taintStr)
	}

	effect := v1.TaintEffect(parts[1])

	// Split key=value part
	keyValueParts := strings.Split(parts[0], "=")
	if len(keyValueParts) != 2 {
		return "", "", "", errors.Errorf("invalid taint key=value format, expected key=value, got %s", parts[0])
	}

	key := keyValueParts[0]
	value := keyValueParts[1]

	// Validate effect
	validEffects := []v1.TaintEffect{v1.TaintEffectNoSchedule, v1.TaintEffectPreferNoSchedule, v1.TaintEffectNoExecute}
	if !slices.Contains(validEffects, effect) {
		return "", "", "", errors.Errorf("invalid taint effect %s, must be one of: NoSchedule, PreferNoSchedule, NoExecute", effect)
	}

	return key, value, effect, nil
}

// cordonAndTaintNode cordons a node and applies the specified taint
func cordonAndTaintNode(ctx context.Context, k8sClient kubernetes.Interface, nodeName, taintStr string, dryRun bool) error {
	logger := log.WithFields(log.Fields{
		"node":     nodeName,
		"taintStr": taintStr,
		"dryRun":   dryRun,
	})

	// Parse the taint string
	taintKey, taintValue, taintEffect, err := parseTaintString(taintStr)
	if err != nil {
		return errors.Wrap(err, "failed to parse taint string")
	}

	// Get the node
	node, err := k8sClient.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return errors.Wrapf(err, "failed to get node %s", nodeName)
	}

	// Check if node is already cordoned and has the taint
	alreadyCordoned := node.Spec.Unschedulable
	alreadyTainted := false

	for _, taint := range node.Spec.Taints {
		if taint.Key == taintKey && taint.Value == taintValue && taint.Effect == taintEffect {
			alreadyTainted = true
			break
		}
	}

	if alreadyCordoned && alreadyTainted {
		logger.Debug("Node is already cordoned and tainted, skipping")
		return nil
	}

	// Cordon the node
	if !alreadyCordoned {
		node.Spec.Unschedulable = true
		logger.Info("Cordoning node")
	}

	// Add the taint if not already present
	if !alreadyTainted {
		newTaint := v1.Taint{
			Key:    taintKey,
			Value:  taintValue,
			Effect: taintEffect,
		}
		node.Spec.Taints = append(node.Spec.Taints, newTaint)
		logger.WithFields(log.Fields{
			"taintKey":    taintKey,
			"taintValue":  taintValue,
			"taintEffect": taintEffect,
		}).Info("Adding taint to node")
	}

	if dryRun {
		logger.Info("DRY-RUN: Would cordon and taint node")
		return nil
	}

	// Update the node
	_, err = k8sClient.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{})
	if err != nil {
		return errors.Wrapf(err, "failed to update node %s with cordon and taint", nodeName)
	}

	logger.Info("Node cordoned and tainted successfully")
	return nil
}

// labelPod applies the specified labels to a pod
func labelPod(ctx context.Context, k8sClient kubernetes.Interface, pod v1.Pod, upgradeStatusLabel, upgradeStatusValue, expiresOnLabel, expiresOnValue, parkedByLabel, parkedByValue string, dryRun bool) error {
	logger := log.WithFields(log.Fields{
		"pod":                pod.Name,
		"namespace":          pod.Namespace,
		"upgradeStatusLabel": upgradeStatusLabel,
		"upgradeStatusValue": upgradeStatusValue,
		"expiresOnLabel":     expiresOnLabel,
		"expiresOnValue":     expiresOnValue,
		"parkedByLabel":      parkedByLabel,
		"parkedByValue":      parkedByValue,
		"dryRun":             dryRun,
	})

	// Get the pod first to check current labels
	currentPod, err := k8sClient.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
	if err != nil {
		return errors.Wrapf(err, "failed to get pod %s/%s", pod.Namespace, pod.Name)
	}

	// Check if the pod already has the labels
	if currentPod.Labels == nil {
		currentPod.Labels = make(map[string]string)
	}

	existingUpgradeStatus := currentPod.Labels[upgradeStatusLabel]
	existingExpiresOn := currentPod.Labels[expiresOnLabel]

	// Check if the pod is already labeled as parked
	if existingUpgradeStatus == "parked" && existingExpiresOn != "" {
		logger.Debug("Pod is already labeled as parked, skipping")
		return nil
	}

	// Apply the labels
	currentPod.Labels[upgradeStatusLabel] = upgradeStatusValue
	currentPod.Labels[expiresOnLabel] = expiresOnValue
	currentPod.Labels[parkedByLabel] = parkedByValue

	if dryRun {
		logger.Info("DRY-RUN: Would label pod")
		return nil
	}

	// Update the pod
	_, err = k8sClient.CoreV1().Pods(pod.Namespace).Update(ctx, currentPod, metav1.UpdateOptions{})
	if err != nil {
		return errors.Wrapf(err, "failed to update pod %s/%s with labels", pod.Namespace, pod.Name)
	}

	logger.Debug("Pod labeled successfully")
	return nil
}

// LabelDriftedNodes labels nodes associated with drifted NodeClaims with the configured labels
func LabelDriftedNodes(ctx context.Context, k8sClient kubernetes.Interface, driftedNodeClaims []KarpenterNodeClaimInfo, cfg config.Config, dryRun bool) error {
	logger := log.WithField("function", "LabelDriftedNodes")

	if len(driftedNodeClaims) == 0 {
		logger.Debug("No drifted nodes to label")
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
	}).Info("Starting to label drifted nodes")

	for _, nodeClaimInfo := range driftedNodeClaims {
		if nodeClaimInfo.NodeName == "" {
			logger.WithField("nodeclaim", nodeClaimInfo.Name).Warn("NodeClaim has no associated node, skipping")
			continue
		}

		// Label the node
		err := labelNode(ctx, k8sClient, nodeClaimInfo.NodeName, cfg.UpgradeStatusLabel, "parked", cfg.ExpiresOnLabel, expirationUnixTime, cfg.ParkedByLabel, "k8s-shredder", dryRun)
		if err != nil {
			logger.WithFields(log.Fields{
				"node":      nodeClaimInfo.NodeName,
				"nodeclaim": nodeClaimInfo.Name,
			}).WithError(err).Error("Failed to label node")
			continue
		}

		// Cordon and taint the node
		err = cordonAndTaintNode(ctx, k8sClient, nodeClaimInfo.NodeName, cfg.ParkedNodeTaint, dryRun)
		if err != nil {
			logger.WithFields(log.Fields{
				"node":      nodeClaimInfo.NodeName,
				"nodeclaim": nodeClaimInfo.Name,
			}).WithError(err).Error("Failed to cordon and taint node")
			// Continue with pod labeling even if cordoning/tainting fails
		}

		logger.WithFields(log.Fields{
			"node":      nodeClaimInfo.NodeName,
			"nodeclaim": nodeClaimInfo.Name,
		}).Info("Successfully processed drifted node (labeled, cordoned, and tainted)")

		// Get eligible pods on the node and label them
		pods, err := getEligiblePodsForNode(ctx, k8sClient, nodeClaimInfo.NodeName)
		if err != nil {
			logger.WithFields(log.Fields{
				"node":      nodeClaimInfo.NodeName,
				"nodeclaim": nodeClaimInfo.Name,
			}).WithError(err).Error("Failed to get pods for node")
			continue
		}

		logger.WithFields(log.Fields{
			"node":      nodeClaimInfo.NodeName,
			"nodeclaim": nodeClaimInfo.Name,
			"podCount":  len(pods),
		}).Info("Found eligible pods to label on drifted node")

		// Label each eligible pod
		for _, pod := range pods {
			err := labelPod(ctx, k8sClient, pod, cfg.UpgradeStatusLabel, "parked", cfg.ExpiresOnLabel, expirationUnixTime, cfg.ParkedByLabel, "k8s-shredder", dryRun)
			if err != nil {
				logger.WithFields(log.Fields{
					"pod":       pod.Name,
					"namespace": pod.Namespace,
					"node":      nodeClaimInfo.NodeName,
				}).WithError(err).Error("Failed to label pod")
				continue
			}

			logger.WithFields(log.Fields{
				"pod":       pod.Name,
				"namespace": pod.Namespace,
				"node":      nodeClaimInfo.NodeName,
			}).Debug("Successfully labeled pod on drifted node")
		}
	}

	return nil
}

// labelNode applies the specified labels to a node
func labelNode(ctx context.Context, k8sClient kubernetes.Interface, nodeName, upgradeStatusLabel, upgradeStatusValue, expiresOnLabel, expiresOnValue, parkedByLabel, parkedByValue string, dryRun bool) error {
	logger := log.WithFields(log.Fields{
		"node":               nodeName,
		"upgradeStatusLabel": upgradeStatusLabel,
		"upgradeStatusValue": upgradeStatusValue,
		"expiresOnLabel":     expiresOnLabel,
		"expiresOnValue":     expiresOnValue,
		"parkedByLabel":      parkedByLabel,
		"parkedByValue":      parkedByValue,
		"dryRun":             dryRun,
	})

	// Get the node first
	node, err := k8sClient.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return errors.Wrapf(err, "failed to get node %s", nodeName)
	}

	// Check if the node already has the labels
	if node.Labels == nil {
		node.Labels = make(map[string]string)
	}

	existingUpgradeStatus := node.Labels[upgradeStatusLabel]
	existingExpiresOn := node.Labels[expiresOnLabel]

	// Check if the node is already labeled as parked
	if existingUpgradeStatus == "parked" && existingExpiresOn != "" {
		logger.Debug("Node is already labeled as parked, skipping")
		return nil
	}

	// Apply the labels
	node.Labels[upgradeStatusLabel] = upgradeStatusValue
	node.Labels[expiresOnLabel] = expiresOnValue
	node.Labels[parkedByLabel] = parkedByValue

	if dryRun {
		logger.Info("DRY-RUN: Would label node")
		return nil
	}

	// Update the node
	_, err = k8sClient.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{})
	if err != nil {
		return errors.Wrapf(err, "failed to update node %s with labels", nodeName)
	}

	logger.Info("Node labeled successfully")
	return nil
}

// ProcessDriftedKarpenterNodes is the main function that combines finding drifted node claims and labeling their nodes
func ProcessDriftedKarpenterNodes(ctx context.Context, appContext *AppContext) error {
	logger := log.WithField("function", "ProcessDriftedKarpenterNodes")

	logger.Info("Starting Karpenter drift detection and node labeling process")

	// Find drifted Karpenter NodeClaims
	driftedNodeClaims, err := FindDriftedKarpenterNodeClaims(ctx, appContext.DynamicK8SClient, appContext.K8sClient, appContext.Config)
	if err != nil {
		return errors.Wrap(err, "failed to find drifted Karpenter NodeClaims")
	}

	if len(driftedNodeClaims) == 0 {
		logger.Info("No drifted Karpenter NodeClaims found")
		return nil
	}

	// Label the nodes associated with drifted NodeClaims
	err = LabelDriftedNodes(ctx, appContext.K8sClient, driftedNodeClaims, appContext.Config, appContext.IsDryRun())
	if err != nil {
		return errors.Wrap(err, "failed to label drifted nodes")
	}

	logger.WithField("processedNodes", len(driftedNodeClaims)).Info("Completed Karpenter drift detection and node labeling process")

	return nil
}
