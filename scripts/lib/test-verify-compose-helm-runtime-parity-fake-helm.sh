#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' 'apiVersion: apps/v1
kind: Deployment
metadata:
  name: eshu-api
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: eshu-mcp-server
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: eshu
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: eshu-resolution-engine
---
apiVersion: batch/v1
kind: Job
metadata:
  name: eshu-schema-bootstrap
---
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: eshu-api-metrics
  labels:
    app.kubernetes.io/component: api
---
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: eshu-mcp-server-metrics
  labels:
    app.kubernetes.io/component: mcp-server
---
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: eshu-ingester-metrics
  labels:
    app.kubernetes.io/component: ingester
---
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: eshu-resolution-engine-metrics
  labels:
    app.kubernetes.io/component: resolution-engine
---
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: eshu-gcp-cloud-collector-metrics
  labels:
    app.kubernetes.io/component: gcp-cloud-collector'
