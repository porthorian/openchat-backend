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

{{/* Coturn resource name. */}}
{{- define "openchat-backend.coturn.fullname" -}}
{{- printf "%s-coturn" (include "openchat-backend.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{/* Coturn selector labels. */}}
{{- define "openchat-backend.coturn.selectorLabels" -}}
{{ include "openchat-backend.selectorLabels" . }}
app.kubernetes.io/component: coturn
{{- end }}

{{/* Coturn labels. */}}
{{- define "openchat-backend.coturn.labels" -}}
{{ include "openchat-backend.labels" . }}
app.kubernetes.io/component: coturn
{{- end }}

{{/* Coturn auth secret name. */}}
{{- define "openchat-backend.coturn.authSecretName" -}}
{{- if .Values.coturn.auth.existingSecret -}}
{{- .Values.coturn.auth.existingSecret -}}
{{- else -}}
{{- printf "%s-auth" (include "openchat-backend.coturn.fullname" .) -}}
{{- end -}}
{{- end }}

{{/* Required coturn TURN URLs as CSV for backend env. */}}
{{- define "openchat-backend.coturn.requiredTurnURLsCSV" -}}
{{- $urls := .Values.coturn.backend.advertisedTURNURLs | default (list) -}}
{{- if eq (len $urls) 0 -}}
{{- fail "coturn.backend.advertisedTURNURLs must be set when coturn.enabled=true and coturn.backend.injectEnv=true" -}}
{{- end -}}
{{- join "," $urls -}}
{{- end }}

{{/* Required coturn STUN URLs (optionally with Google fallback) as CSV for backend env. */}}
{{- define "openchat-backend.coturn.requiredStunURLsCSV" -}}
{{- $stun := .Values.coturn.backend.advertisedSTUNURLs | default (list) -}}
{{- if eq (len $stun) 0 -}}
{{- fail "coturn.backend.advertisedSTUNURLs must be set when coturn.enabled=true and coturn.backend.injectEnv=true" -}}
{{- end -}}
{{- $effective := $stun -}}
{{- if .Values.coturn.backend.appendGoogleStunFallback -}}
{{- $google := .Values.coturn.backend.googleStunFallbackURLs | default (list) -}}
{{- $effective = concat $effective $google -}}
{{- end -}}
{{- join "," $effective -}}
{{- end }}
