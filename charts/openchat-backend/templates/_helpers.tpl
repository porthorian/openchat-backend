{{/* Expand the chart name. */}}
{{- define "openchat-backend.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/* Create a default fully qualified app name. */}}
{{- define "openchat-backend.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := include "openchat-backend.name" . }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/* Create chart name and version for labels. */}}
{{- define "openchat-backend.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{- end }}

{{/* Common labels. */}}
{{- define "openchat-backend.labels" -}}
helm.sh/chart: {{ include "openchat-backend.chart" . }}
{{ include "openchat-backend.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/* Selector labels. */}}
{{- define "openchat-backend.selectorLabels" -}}
app.kubernetes.io/name: {{ include "openchat-backend.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/* Service account name. */}}
{{- define "openchat-backend.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "openchat-backend.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/* Gateway resource name when chart manages one. */}}
{{- define "openchat-backend.gatewayName" -}}
{{- default (include "openchat-backend.fullname" .) .Values.gateway.gateway.name }}
{{- end }}
