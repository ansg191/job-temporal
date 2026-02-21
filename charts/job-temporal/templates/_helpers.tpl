{{/*
Expand the name of the chart.
*/}}
{{- define "job-temporal.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "job-temporal.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart version string used in labels.
*/}}
{{- define "job-temporal.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels applied to all resources.
*/}}
{{- define "job-temporal.labels" -}}
helm.sh/chart: {{ include "job-temporal.chart" . }}
{{ include "job-temporal.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels — used for pod selection in Deployments and Services.
*/}}
{{- define "job-temporal.selectorLabels" -}}
app.kubernetes.io/name: {{ include "job-temporal.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Service account name.
*/}}
{{- define "job-temporal.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "job-temporal.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Resolve the Temporal server address.
Defaults to the in-chart temporal service if not overridden.
*/}}
{{- define "job-temporal.temporalAddress" -}}
{{- if .Values.temporalAddress }}
{{- .Values.temporalAddress }}
{{- else }}
{{- printf "%s-temporal:7233" (include "job-temporal.fullname" .) }}
{{- end }}
{{- end }}

{{/*
Image helper — renders "repository:tag" for a given image config.
Usage: include "job-temporal.image" .Values.server.image
*/}}
{{- define "job-temporal.image" -}}
{{- $tag := .tag | default "main" }}
{{- printf "%s:%s" .repository $tag }}
{{- end }}
