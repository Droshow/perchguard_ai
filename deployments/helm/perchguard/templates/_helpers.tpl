{{/*
Expand the name of the chart.
*/}}
{{- define "perchguard.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "perchguard.fullname" -}}
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
Create chart label.
*/}}
{{- define "perchguard.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels applied to every resource.
*/}}
{{- define "perchguard.labels" -}}
helm.sh/chart: {{ include "perchguard.chart" . }}
{{ include "perchguard.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels — used in Deployment.spec.selector and Service.spec.selector.
*/}}
{{- define "perchguard.selectorLabels" -}}
app.kubernetes.io/name: {{ include "perchguard.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
The listen address: HTTPS when tls.enabled, HTTP otherwise.
*/}}
{{- define "perchguard.listenAddr" -}}
{{- if .Values.tls.enabled -}}:8443{{- else -}}:8080{{- end }}
{{- end }}

{{/*
The port number the container listens on.
*/}}
{{- define "perchguard.containerPort" -}}
{{- if .Values.tls.enabled -}}8443{{- else -}}8080{{- end }}
{{- end }}

{{/*
The healthz scheme for probes.
*/}}
{{- define "perchguard.probeScheme" -}}
{{- if .Values.tls.enabled -}}HTTPS{{- else -}}HTTP{{- end }}
{{- end }}

{{/*
ServiceAccount name.
*/}}
{{- define "perchguard.serviceAccountName" -}}
{{- include "perchguard.fullname" . }}
{{- end }}
