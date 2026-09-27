{{/*
Имя чарта (без версии релиза) — нужно как основа для fullname ниже
и для лейбла app.kubernetes.io/name.
*/}}
{{- define "gophprofile.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Полное имя — им будут называться все ресурсы (Deployment, Service и т.д.),
с суффиксом -server/-worker сверху. Helm-конвенция: если имя релиза уже
содержит имя чарта, не дублируем его (иначе получилось бы что-то вроде
"gophprofile-gophprofile-server" при `helm install gophprofile ./k8s`).
*/}}
{{- define "gophprofile.fullname" -}}
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

{{/* Строка "имя-чарта-версия" для лейбла helm.sh/chart. */}}
{{- define "gophprofile.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Общие лейблы — вешаем на КАЖДЫЙ ресурс чарта. helm.sh/chart и
app.kubernetes.io/managed-by нужны не приложению, а самому Helm/людям,
которые потом смотрят `kubectl get all -l app.kubernetes.io/managed-by=Helm`
и сразу видят, что этим ресурсом управляет Helm, а не создан руками.
*/}}
{{- define "gophprofile.labels" -}}
helm.sh/chart: {{ include "gophprofile.chart" . }}
{{ include "gophprofile.selectorLabels" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Селекторные лейблы — минимальный набор, по которому Deployment находит
свои Pod'ы (Service — тоже). В отличие от gophprofile.labels выше,
это НЕЛЬЗЯ менять после первого деплоя релиза: Deployment.spec.selector
неизменяем, попытка поменять состав лейблов здесь потребует пересоздания
Deployment'а целиком.
*/}}
{{- define "gophprofile.selectorLabels" -}}
app.kubernetes.io/name: {{ include "gophprofile.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Имя ServiceAccount'а, который будут использовать Pod'ы server/worker.
Если serviceAccount.create=false — предполагается, что ServiceAccount
с таким именем уже существует в кластере и создан отдельно.
*/}}
{{- define "gophprofile.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "gophprofile.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}
