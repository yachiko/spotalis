{{/*
Expand the name of the chart.
*/}}
{{- define "spotalis.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "spotalis.fullname" -}}
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
{{- define "spotalis.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "spotalis.labels" -}}
helm.sh/chart: {{ include "spotalis.chart" . }}
{{ include "spotalis.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "spotalis.selectorLabels" -}}
app.kubernetes.io/name: {{ include "spotalis.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "spotalis.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "spotalis.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Create the image name
*/}}
{{- define "spotalis.image" -}}
{{- $registry := .Values.image.registry -}}
{{- $repository := .Values.image.repository -}}
{{- $tag := .Values.image.tag | default .Chart.AppVersion -}}
{{- printf "%s/%s:%s" $registry $repository $tag -}}
{{- end }}

{{/*
Create the webhook certificate secret name
*/}}
{{- define "spotalis.webhookCertSecretName" -}}
{{- if .Values.webhook.tls.existingSecret }}
{{- .Values.webhook.tls.existingSecret }}
{{- else }}
{{- printf "%s-webhook-cert" (include "spotalis.fullname" .) }}
{{- end }}
{{- end }}

{{/*
Determine if leader election should be enabled
*/}}
{{- define "spotalis.leaderElectionEnabled" -}}
{{- if gt (int .Values.replicaCount) 1 }}
{{- "true" }}
{{- else }}
{{- .Values.config.operator.leaderElection.enabled | default "false" }}
{{- end }}
{{- end }}

{{/*
Create image pull secrets
*/}}
{{- define "spotalis.imagePullSecrets" -}}
{{- if .Values.imagePullSecrets }}
imagePullSecrets:
{{- range .Values.imagePullSecrets }}
  - name: {{ . }}
{{- end }}
{{- end }}
{{- end }}
