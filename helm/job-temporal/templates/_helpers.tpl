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
Create chart name and version as used by the chart label.
*/}}
{{- define "job-temporal.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
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
Selector labels
*/}}
{{- define "job-temporal.selectorLabels" -}}
app.kubernetes.io/name: {{ include "job-temporal.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "job-temporal.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "job-temporal.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Name of the shared ExternalSecret (DATABASE_URL).
*/}}
{{- define "job-temporal.sharedSecretName" -}}
{{- printf "%s-shared" (include "job-temporal.fullname" .) }}
{{- end }}

{{/*
Name of the server ExternalSecret (GITHUB_WEBHOOK_SECRET).
*/}}
{{- define "job-temporal.serverSecretName" -}}
{{- printf "%s-server" (include "job-temporal.fullname" .) }}
{{- end }}

{{/*
Name of the worker ExternalSecret.
*/}}
{{- define "job-temporal.workerSecretName" -}}
{{- printf "%s-worker" (include "job-temporal.fullname" .) }}
{{- end }}
