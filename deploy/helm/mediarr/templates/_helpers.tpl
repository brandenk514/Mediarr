{{/*
Chart metadata + naming.
*/}}
{{- define "mediarr.name" -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "mediarr.fullname" -}}
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

{{- define "mediarr.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "mediarr.labels" -}}
helm.sh/chart: {{ include "mediarr.chart" . }}
{{ include "mediarr.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: mediarr
{{- end }}

{{- define "mediarr.selectorLabels" -}}
app.kubernetes.io/name: {{ include "mediarr.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Component name helper: <fullname>-<component>.
*/}}
{{- define "mediarr.component" -}}
{{- printf "%s-%s" (include "mediarr.fullname" .) .component }}
{{- end }}

{{/*
Postgres DSN for the app (MEDIARR_POSTGRES_DSN).
*/}}
{{- define "mediarr.pgDsn" -}}
{{- printf "postgres://%s:%s@%s-postgres:5432/%s?sslmode=disable" .Values.postgres.auth.user (urlquery .Values.secrets.postgresPassword) (include "mediarr.fullname" .) .Values.postgres.auth.database -}}
{{- end }}

{{/*
Redis URL for the app (MEDIARR_REDIS_URL).
*/}}
{{- define "mediarr.redisUrl" -}}
{{- $auth := "" -}}
{{- if .Values.secrets.redisPassword -}}
{{- $auth = printf ":%s@" (urlquery .Values.secrets.redisPassword) -}}
{{- end -}}
{{- printf "redis://%s%s-redis:6379/%d" $auth (include "mediarr.fullname" .) (int .Values.redis.database) -}}
{{- end }}

{{/*
Shared Pod security context (PLAN §9): nonroot, no privilege escalation,
drop ALL capabilities, seccomp RuntimeDefault.
*/}}
{{- define "mediarr.podSecurityContext" -}}
runAsNonRoot: true
seccompProfile:
  type: RuntimeDefault
{{- end }}

{{/*
Shared container security context.
*/}}
{{- define "mediarr.containerSecurityContext" -}}
allowPrivilegeEscalation: false
capabilities:
  drop:
    - ALL
{{- end }}
