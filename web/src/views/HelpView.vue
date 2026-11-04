<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { getSourceTypes } from '@/api/monitors'
import { getNotifierTypes } from '@/api/channels'
import type { NotifierMeta } from '@/api/channels'
import type { SourceMeta } from '@/api/types'
import { useMetaText } from '@/composables/useMetaText'
import { useTemplatePresets, useTemplateVariables } from '@/constants/notifyTemplates'

const { t, te } = useI18n()
const { label: metaLabel, help: metaHelp, placeholder: metaPlaceholder } = useMetaText()
const route = useRoute()
const router = useRouter()

const snackbar = ref(false)
const snackbarText = ref('')

// ---------- Tab 导航与 ?tab= 深链 ----------

const tabKeys = ['quickstart', 'sources', 'channels', 'compare', 'filters', 'nvchecker', 'api'] as const
type TabKey = (typeof tabKeys)[number]

const tabs = computed(() => [
  { key: 'quickstart', title: t('help.tabs.quickstart'), icon: 'mdi-rocket-launch-outline' },
  { key: 'sources', title: t('help.tabs.sources'), icon: 'mdi-radar' },
  { key: 'channels', title: t('help.tabs.channels'), icon: 'mdi-bell-ring-outline' },
  { key: 'compare', title: t('help.tabs.compare'), icon: 'mdi-scale-balance' },
  { key: 'filters', title: t('help.tabs.filters'), icon: 'mdi-filter-variant' },
  { key: 'nvchecker', title: t('help.tabs.nvchecker'), icon: 'mdi-import' },
  { key: 'api', title: t('help.tabs.api'), icon: 'mdi-code-json' },
] as const)

function isTabKey(v: unknown): v is TabKey {
  return typeof v === 'string' && (tabKeys as readonly string[]).includes(v)
}

const activeTab = ref<TabKey>('quickstart')

// 外部深链（如渠道页「查看完整文档」/help?tab=channels）→ 切到对应 Tab
watch(() => route.query.tab, (raw) => {
  const v = Array.isArray(raw) ? raw[0] : raw
  if (isTabKey(v) && v !== activeTab.value) activeTab.value = v
}, { immediate: true })

// 手工切 Tab → 同步 URL（replace，不堆历史）
watch(activeTab, (next) => {
  if (route.query.tab !== next) router.replace({ query: { ...route.query, tab: next } })
})

// ---------- 后端注册表数据（字段表零漂移） ----------

const loading = ref(true)
const sources = ref<SourceMeta[]>([])
const channels = ref<NotifierMeta[]>([])

onMounted(async () => {
  try {
    const [s, c] = await Promise.all([getSourceTypes(), getNotifierTypes()])
    sources.value = s.items
    channels.value = c.items
  } catch (e) {
    snackbarText.value = t('help.loadFailed', { error: e instanceof Error ? e.message : String(e) })
    snackbar.value = true
  } finally {
    loading.value = false
  }
})

async function copyText(text: string, what: string) {
  try {
    await navigator.clipboard.writeText(text)
    snackbarText.value = t('common.copiedToClipboard', { what })
  } catch {
    snackbarText.value = t('common.copyFailed')
  }
  snackbar.value = true
}

// ---------- 监控源分类与示例 ----------

const sourceCategories = computed(() => [
  { key: 'all', title: t('help.sources.categories.all'), types: [] as string[] },
  { key: 'vcs', title: t('help.sources.categories.vcs'), types: ['github_release', 'github_tag', 'gitlab_release', 'gitea_release', 'git_tag', 'bitbucket', 'pagure'] },
  { key: 'distro', title: t('help.sources.categories.distro'), types: ['aur', 'arch_official', 'debianpkg', 'ubuntupkg', 'repology', 'anitya'] },
  { key: 'lang', title: t('help.sources.categories.lang'), types: ['go_module', 'npm', 'pypi', 'crates', 'maven', 'packagist', 'rubygems', 'cpan', 'cran', 'hackage'] },
  { key: 'container', title: t('help.sources.categories.container'), types: ['container', 'docker_tag', 'openvsx', 'vsmarketplace', 'snapcraft'] },
  { key: 'generic', title: t('help.sources.categories.generic'), types: ['http_json', 'http_regex', 'http_header', 'manual', 'cmd'] },
])

const sourceCategory = ref('all')
const sourceSearch = ref('')

const filteredSources = computed(() => {
  const cat = sourceCategories.value.find(c => c.key === sourceCategory.value)
  const q = sourceSearch.value.trim().toLowerCase()
  return sources.value.filter((s) => {
    if (cat && cat.types.length > 0 && !cat.types.includes(s.type)) return false
    if (!q) return true
    return [s.type, metaLabel(s), ...s.fields.flatMap(f => [f.key, metaLabel(f), metaHelp(f), metaPlaceholder(f)])]
      .some(v => v.toLowerCase().includes(q))
  })
})

// 各源配置示例：键名与后端 Meta 字段一致，可直接粘贴后再改值
const sourceExamples: Record<string, string> = {
  github_release: '{"repo": "torvalds/linux"}',
  github_tag: '{"repo": "vuejs/core", "tag_pattern": "^v[0-9.]+$"}',
  gitlab_release: '{"project": "gitlab-org/gitlab"}',
  gitea_release: '{"host": "https://gitea.com", "repo": "owner/repo"}',
  git_tag: '{"url": "https://git.kernel.org/pub/scm/linux/kernel/git/torvalds/linux.git", "tag_pattern": "^v[0-9.]+$"}',
  bitbucket: '{"repo": "atlassian/python-bitbucket"}',
  pagure: '{"repo": "pagure"}',
  aur: '{"pkgname": "yay"}',
  arch_official: '{"pkgname": "linux", "repo": "core"}',
  debianpkg: '{"pkgname": "ripgrep", "suite": "sid", "strip_release": true}',
  ubuntupkg: '{"pkgname": "curl", "suite": "noble", "strip_release": true}',
  repology: '{"project": "ripgrep", "repo": "arch", "subrepo": "extra"}',
  anitya: '{"project": "13815"}',
  go_module: '{"module": "github.com/gin-gonic/gin"}',
  npm: '{"package": "vue", "dist_tag": "latest"}',
  pypi: '{"project": "requests"}',
  crates: '{"crate": "tokio"}',
  maven: '{"group": "org.apache.commons", "artifact": "commons-lang3"}',
  packagist: '{"package": "monolog/monolog"}',
  rubygems: '{"gem": "rails"}',
  cpan: '{"module": "Mojolicious"}',
  cran: '{"package": "ggplot2"}',
  hackage: '{"package": "pandoc"}',
  container: '{"image": "ghcr.io/owner/repo", "tag_regex": "^v[0-9.]+$"}',
  docker_tag: '{"image": "nginx", "tag_regex": "^1[0-9.]+\\\\.[0-9.]+$"}',
  openvsx: '{"extension": "rust-lang.rust-analyzer"}',
  vsmarketplace: '{"extension": "golang.go"}',
  snapcraft: '{"snap": "canonical-livepatch", "channel": "stable"}',
  http_json: '{"url": "https://api.example.com/ver", "path": "data.version"}',
  http_regex: '{"url": "https://example.com/download", "regex": "v([0-9.]+)"}',
  http_header: '{"url": "https://github.com/owner/repo/releases/latest", "header": "Location", "regex": "v([0-9.]+)"}',
  manual: '{"version": "1.0.0", "url": "https://example.com"}',
  cmd: '{"cmd": "curl -s https://example.com/ver.txt", "timeout": 15}',
}

function sourceNote(type: string): string {
  return te(`help.sourceNotes.${type}`) ? t(`help.sourceNotes.${type}`) : ''
}

function channelHint(type: string): string {
  return te(`help.channelHints.${type}`) ? t(`help.channelHints.${type}`) : ''
}

// ---------- 字段展示辅助 ----------

// 需要展示的 Go 模板片段：宁可放在 script 里，也不要把 {{ }} 直接写进 Vue 模板（会被当作插值解析）
const tagsRangeExample = '{{ range $i, $t := .Monitor.Tags }}{{ if $i }}, {{ end }}{{ $t }}{{ end }}'

type FieldLike = {
  key: string
  label: string
  label_en?: string
  required: boolean
  options?: string[]
  placeholder?: string
  placeholder_en?: string
  help?: string
  help_en?: string
  default?: unknown
}

function fieldHint(f: FieldLike): string {
  return metaHelp(f) || metaPlaceholder(f) || '—'
}

function fieldDefault(f: FieldLike): string {
  if (f.options && f.options.length > 0) return f.options.filter(Boolean).join(' / ')
  if (f.default === undefined || f.default === null) return ''
  return String(f.default)
}

// ---------- 状态机与对比事件（与后端常量一致） ----------

const states = computed(() => [
  { code: 'up_to_date', label: t('status.up_to_date'), desc: t('help.states.up_to_date') },
  { code: 'has_update', label: t('status.has_update'), desc: t('help.states.has_update') },
  { code: 'error', label: t('status.error'), desc: t('help.states.error') },
  { code: 'missing', label: t('status.missing'), desc: t('help.states.missing') },
])

const compareEvents = computed(() => [
  { code: 'entered_lagging', desc: t('help.compareEvents.entered_lagging') },
  { code: 'gap_grew', desc: t('help.compareEvents.gap_grew') },
  { code: 'caught_up', desc: t('help.compareEvents.caught_up') },
])

const compareStates = computed(() => [
  { code: 'lagging', desc: t('help.compareStates.lagging') },
  { code: 'in_sync', desc: t('help.compareStates.in_sync') },
  { code: 'ahead', desc: t('help.compareStates.ahead') },
])

// ---------- 版本清洗字段（名称与 API / 编辑页一致） ----------

const filterDocs = computed(() => [
  { field: 'version_prefix', label: t('monitors.edit.prefix'), desc: t('help.filters.prefixDesc'), example: t('help.filters.prefixExample') },
  { field: 'version_suffix', label: t('monitors.edit.suffix'), desc: t('help.filters.suffixDesc'), example: t('help.filters.suffixExample') },
  { field: 'from_pattern', label: t('monitors.edit.fromPattern'), desc: t('help.filters.fromDesc'), example: t('help.filters.fromExample') },
  { field: 'to_pattern', label: t('monitors.edit.toPattern'), desc: t('help.filters.toDesc'), example: t('help.filters.toExample') },
  { field: 'version_include_regex', label: t('monitors.edit.include'), desc: t('help.filters.includeDesc'), example: t('help.filters.includeExample') },
  { field: 'version_ignore_regex', label: t('monitors.edit.ignore'), desc: t('help.filters.ignoreDesc'), example: t('help.filters.ignoreExample') },
])

// ---------- nvchecker 迁移映射（与 importer 实现一致） ----------

const nvcheckerMappings = computed(() => [
  { from: 'github', to: 'github_release / github_tag' },
  { from: 'gitlab', to: 'gitlab_release' },
  { from: 'gitea', to: 'gitea_release' },
  { from: 'aur', to: 'aur' },
  { from: 'archpkg / pacman / alpm', to: 'arch_official' },
  { from: 'pypi', to: 'pypi' },
  { from: 'npm', to: 'npm' },
  { from: 'cratesio', to: 'crates' },
  { from: t('help.nvchecker.goproxyFrom'), to: 'go_module' },
  { from: 'regex / htmlparser', to: t('help.nvchecker.regexTo') },
  { from: 'httpheader', to: 'http_header' },
  { from: 'git / git_dulwich / git_pygit2', to: 'git_tag' },
  { from: 'bitbucket / pagure', to: 'bitbucket / pagure' },
  { from: 'container', to: 'container' },
  { from: 'debianpkg / ubuntupkg', to: 'debianpkg / ubuntupkg' },
  { from: 'cpan / cran / hackage', to: 'cpan / cran / hackage' },
  { from: 'repology / anitya', to: 'repology / anitya' },
  { from: 'gems / rubygems', to: 'rubygems' },
  { from: 'maven / packagist', to: 'maven / packagist' },
  { from: 'openvsx / vsmarketplace / snapcraft / manual', to: t('help.nvchecker.sameNameTo') },
  { from: 'cmd', to: t('help.nvchecker.cmdTo') },
])

const templateVariables = useTemplateVariables()
const templatePresets = useTemplatePresets()

const apiOrigin = computed(() => location.origin)
const apiExamples = computed(() => [
  {
    title: t('help.api.listMonitorsDesc'),
    method: 'GET',
    path: '/api/v1/monitors',
    cmd: `curl -s -H "Authorization: Bearer <your-token>" "${apiOrigin.value}/api/v1/monitors"`,
  },
  {
    title: t('help.api.checkNowDesc'),
    method: 'POST',
    path: '/api/v1/monitors/{id}/check',
    cmd: `curl -s -X POST -H "Authorization: Bearer <your-token>" "${apiOrigin.value}/api/v1/monitors/1/check"`,
  },
  {
    title: t('help.api.ackDesc'),
    method: 'POST',
    path: '/api/v1/monitors/{id}/ack',
    cmd: `curl -s -X POST -H "Authorization: Bearer <your-token>" "${apiOrigin.value}/api/v1/monitors/1/ack"`,
  },
  {
    title: t('help.api.getChangesDesc'),
    method: 'GET',
    path: '/api/v1/monitors/{id}/changes',
    cmd: `curl -s -H "Authorization: Bearer <your-token>" "${apiOrigin.value}/api/v1/monitors/1/changes"`,
  },
])

const crontabExample = computed(
  () => `0 */2 * * * curl -s -X POST -H "Authorization: Bearer <your-token>" "${apiOrigin.value}/api/v1/monitors/1/check" > /dev/null`
)
</script>

<template>
  <div>
    <div class="mb-4">
      <div class="text-h5">{{ t('help.title') }}</div>
      <div class="text-body-2 text-medium-emphasis">{{ t('help.subtitle') }}</div>
    </div>

    <v-tabs v-model="activeTab" show-arrows class="mb-4">
      <v-tab v-for="tab in tabs" :key="tab.key" :value="tab.key" :prepend-icon="tab.icon" class="text-none">
        {{ tab.title }}
      </v-tab>
    </v-tabs>

    <v-window v-model="activeTab">
      <!-- ================= 快速入门 ================= -->
      <v-window-item value="quickstart">
        <v-row>
          <v-col cols="12" md="4">
            <v-card class="pa-4 h-100">
              <div class="text-subtitle-1 font-weight-bold mb-2">{{ t('help.quickstart.step1Title') }}</div>
              <p class="text-body-2 mb-3">{{ t('help.quickstart.step1Desc') }}</p>
              <v-btn size="small" color="primary" variant="tonal" prepend-icon="mdi-bell-plus-outline" to="/channels">
                {{ t('help.quickstart.step1Btn') }}
              </v-btn>
            </v-card>
          </v-col>
          <v-col cols="12" md="4">
            <v-card class="pa-4 h-100">
              <div class="text-subtitle-1 font-weight-bold mb-2">{{ t('help.quickstart.step2Title') }}</div>
              <p class="text-body-2 mb-3">{{ t('help.quickstart.step2Desc') }}</p>
              <v-btn size="small" color="primary" variant="tonal" prepend-icon="mdi-radar" to="/monitors/new">
                {{ t('help.quickstart.step2Btn') }}
              </v-btn>
            </v-card>
          </v-col>
          <v-col cols="12" md="4">
            <v-card class="pa-4 h-100">
              <div class="text-subtitle-1 font-weight-bold mb-2">{{ t('help.quickstart.step3Title') }}</div>
              <p class="text-body-2 mb-3">{{ t('help.quickstart.step3Desc') }}</p>
              <v-btn size="small" color="primary" variant="tonal" prepend-icon="mdi-view-dashboard-outline" to="/">
                {{ t('help.quickstart.step3Btn') }}
              </v-btn>
            </v-card>
          </v-col>
        </v-row>

        <v-card class="pa-4 mt-4">
          <div class="text-subtitle-1 font-weight-bold mb-3">{{ t('help.quickstart.conceptsTitle') }}</div>
          <v-table density="comfortable">
            <thead>
              <tr>
                <th style="width: 200px">{{ t('help.quickstart.concept') }}</th>
                <th>{{ t('help.quickstart.description') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr>
                <td>{{ t('help.quickstart.currentBaseline') }}</td>
                <td>{{ t('help.quickstart.currentBaselineDesc') }}</td>
              </tr>
              <tr>
                <td>{{ t('help.quickstart.latestDetected') }}</td>
                <td>{{ t('help.quickstart.latestDetectedDesc') }}</td>
              </tr>
              <tr>
                <td>{{ t('help.quickstart.ackUpdate') }}</td>
                <td>{{ t('help.quickstart.ackUpdateDesc') }}</td>
              </tr>
              <tr>
                <td>{{ t('help.quickstart.interval') }}</td>
                <td>{{ t('help.quickstart.intervalDesc') }}</td>
              </tr>
              <tr>
                <td>{{ t('help.quickstart.cooldown') }}</td>
                <td>{{ t('help.quickstart.cooldownDesc') }}</td>
              </tr>
            </tbody>
          </v-table>
        </v-card>

        <v-card class="pa-4 mt-4">
          <div class="text-subtitle-1 font-weight-bold mb-3">{{ t('help.quickstart.stateMachine') }}</div>
          <v-table density="comfortable">
            <thead>
              <tr>
                <th style="width: 160px">{{ t('help.quickstart.state') }}</th>
                <th style="width: 160px">{{ t('help.quickstart.internalValue') }}</th>
                <th>{{ t('help.quickstart.meaning') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="s in states" :key="s.code">
                <td>{{ s.label }}</td>
                <td><code class="text-caption">{{ s.code }}</code></td>
                <td>{{ s.desc }}</td>
              </tr>
            </tbody>
          </v-table>
        </v-card>
      </v-window-item>

      <!-- ================= 监控源手册 ================= -->
      <v-window-item value="sources">
        <div class="d-flex flex-wrap align-center ga-2 mb-2">
          <v-text-field
            v-model="sourceSearch"
            :label="t('help.sources.search')"
            prepend-inner-icon="mdi-magnify"
            clearable
            hide-details
            density="comfortable"
            style="max-width: 320px"
          />
          <div class="text-body-2 text-medium-emphasis">
            {{ t('help.sources.count', { total: sources.length, shown: filteredSources.length }) }}
          </div>
        </div>

        <div class="d-flex flex-wrap ga-1 mb-4">
          <v-chip
            v-for="c in sourceCategories"
            :key="c.key"
            size="small"
            label
            :color="sourceCategory === c.key ? 'primary' : undefined"
            :variant="sourceCategory === c.key ? 'flat' : 'tonal'"
            @click="sourceCategory = c.key"
          >
            {{ c.title }}
          </v-chip>
        </div>

        <v-alert v-if="loading" type="info" variant="tonal" density="compact">{{ t('help.sources.loading') }}</v-alert>
        <v-alert v-else-if="filteredSources.length === 0" type="info" variant="tonal" density="compact">
          {{ t('help.sources.noMatch') }}
        </v-alert>

        <v-expansion-panels v-else variant="accordion" multiple>
          <v-expansion-panel v-for="s in filteredSources" :key="s.type" :value="s.type">
            <v-expansion-panel-title>
              <v-icon class="me-2">{{ s.icon ?? 'mdi-radar' }}</v-icon>
              <span class="font-weight-medium">{{ metaLabel(s) }}</span>
              <code class="text-caption ms-3 text-medium-emphasis">{{ s.type }}</code>
            </v-expansion-panel-title>
            <v-expansion-panel-text>
              <v-table density="compact" class="mb-3">
                <thead>
                  <tr>
                    <th style="width: 180px">{{ t('help.sources.field') }}</th>
                    <th>{{ t('help.channels.description') }}</th>
                    <th style="width: 180px">{{ t('help.sources.optionsDefault') }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="f in s.fields" :key="f.key">
                    <td>
                      <code class="text-caption">{{ f.key }}</code>
                      <span v-if="f.required" class="text-error ms-1">*</span>
                    </td>
                    <td>
                      {{ metaLabel(f) }}
                      <div v-if="fieldHint(f) !== '—'" class="text-caption text-medium-emphasis">{{ fieldHint(f) }}</div>
                    </td>
                    <td class="text-caption">{{ fieldDefault(f) || '—' }}</td>
                  </tr>
                </tbody>
              </v-table>

              <div class="d-flex align-center mb-1">
                <span class="text-subtitle-2">{{ t('help.sources.configExample') }}</span>
                <v-spacer />
                <v-btn
                  size="x-small"
                  variant="text"
                  prepend-icon="mdi-content-copy"
                  @click="copyText(sourceExamples[s.type] ?? '', t('help.sources.configExample'))"
                >
                  {{ t('help.sources.copyJson') }}
                </v-btn>
              </div>
              <pre class="pa-3 bg-surface-lighten-2 rounded text-caption font-mono text-pre-wrap mb-2">{{ sourceExamples[s.type] ?? '—' }}</pre>

              <v-alert v-if="sourceNote(s.type)" type="info" variant="tonal" density="compact">
                {{ sourceNote(s.type) }}
              </v-alert>
            </v-expansion-panel-text>
          </v-expansion-panel>
        </v-expansion-panels>
      </v-window-item>

      <!-- ================= 渠道与模板 ================= -->
      <v-window-item value="channels">
        <div class="text-subtitle-1 font-weight-bold mb-1">{{ t('help.channels.title', { n: channels.length }) }}</div>
        <div class="text-body-2 text-medium-emphasis mb-3">{{ t('help.channels.desc') }}</div>

        <v-row dense>
          <v-col v-for="c in channels" :key="c.type" cols="12" md="6">
            <v-card class="pa-4 h-100">
              <div class="d-flex align-center mb-1">
                <v-icon class="me-2">{{ c.icon ?? 'mdi-bell-outline' }}</v-icon>
                <span class="text-subtitle-1 font-weight-bold">{{ metaLabel(c) }}</span>
                <code class="text-caption ms-2 text-medium-emphasis">{{ c.type }}</code>
              </div>
              <div v-if="channelHint(c.type)" class="text-body-2 mb-2">{{ channelHint(c.type) }}</div>
              <v-table density="compact">
                <thead>
                  <tr>
                    <th style="width: 150px">{{ t('help.channels.field') }}</th>
                    <th>{{ t('help.channels.description') }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="f in c.fields" :key="f.key">
                    <td>
                      <code class="text-caption">{{ f.key }}</code>
                      <span v-if="f.required" class="text-error ms-1">*</span>
                    </td>
                    <td>
                      {{ metaLabel(f) }}
                      <div v-if="metaHelp(f)" class="text-caption text-medium-emphasis">{{ metaHelp(f) }}</div>
                    </td>
                  </tr>
                </tbody>
              </v-table>
            </v-card>
          </v-col>
        </v-row>

        <v-btn class="mt-3" size="small" color="primary" variant="tonal" prepend-icon="mdi-bell-plus-outline" to="/channels">
          {{ t('help.channels.goConfig') }}
        </v-btn>

        <v-divider class="my-6" />

        <div class="text-subtitle-1 font-weight-bold mb-1">{{ t('help.channels.templateSyntax') }}</div>
        <div class="text-body-2 text-medium-emphasis mb-3">{{ t('help.channels.templateSyntaxDesc') }}</div>

        <v-card class="pa-4 mb-3" variant="outlined">
          <div class="text-subtitle-2 font-weight-bold mb-2">{{ t('help.channels.variables') }}</div>
          <div v-for="g in templateVariables" :key="g.group" class="mb-3">
            <div class="text-caption text-medium-emphasis mb-1">{{ g.group }}</div>
            <v-table density="compact">
              <tbody>
                <tr v-for="f in g.fields" :key="f.key">
                  <td style="width: 46%">
                    <code class="text-caption font-mono">{{ f.key }}</code>
                  </td>
                  <td>{{ f.desc }}</td>
                </tr>
              </tbody>
            </v-table>
          </div>
          <v-alert type="info" variant="tonal" density="compact">
            <div class="text-body-2">
              {{ t('help.channels.syntax1') }}<code class="font-mono">if / else / eq / range</code>；
              {{ t('help.channels.syntax2') }}<code class="font-mono">upper</code>{{ t('help.channels.syntax3') }}<code class="font-mono">joinTags</code>{{ t('help.channels.syntax4') }}<code class="font-mono">compareStateText</code>{{ t('help.channels.syntax5') }}
              <code class="font-mono">Tags</code>{{ t('help.channels.syntax6') }}<code class="font-mono">[tag1 tag2]</code>{{ t('help.channels.syntax7') }}
              <code class="font-mono">{{ tagsRangeExample }}</code>。
            </div>
          </v-alert>
        </v-card>

        <div class="text-subtitle-2 font-weight-bold mb-2">{{ t('help.channels.presetsTitle') }}</div>
        <v-row dense>
          <v-col v-for="preset in templatePresets" :key="preset.key" cols="12">
            <v-card class="pa-3">
              <div class="d-flex align-center mb-1">
                <span class="text-body-2 font-weight-bold">{{ preset.name }}</span>
                <v-spacer />
                <v-btn
                  size="x-small"
                  color="primary"
                  variant="tonal"
                  prepend-icon="mdi-content-copy"
                  @click="copyText(preset.template, preset.name)"
                >
                  {{ t('common.copy') }}
                </v-btn>
              </div>
              <div class="text-caption text-medium-emphasis mb-2">{{ preset.desc }}</div>
              <pre class="pa-2 bg-surface-lighten-2 rounded text-caption font-mono text-pre-wrap">{{ preset.template }}</pre>
            </v-card>
          </v-col>
        </v-row>
      </v-window-item>

      <!-- ================= 双轨对比 ================= -->
      <v-window-item value="compare">
        <v-card class="pa-4 mb-4">
          <div class="text-subtitle-1 font-weight-bold mb-2">{{ t('help.compare.problemTitle') }}</div>
          <p class="text-body-2 mb-0">{{ t('help.compare.problemDesc') }}</p>
        </v-card>

        <v-card class="pa-4 mb-4">
          <div class="text-subtitle-1 font-weight-bold mb-3">{{ t('help.compare.stepsTitle') }}</div>
          <ol class="text-body-2 ps-6 mb-0">
            <li class="mb-2">{{ t('help.compare.step1') }}</li>
            <li class="mb-2">{{ t('help.compare.step2') }}</li>
            <li>{{ t('help.compare.step3') }}</li>
          </ol>
          <v-alert type="info" variant="tonal" density="compact" class="mt-3">
            {{ t('help.compare.chainHint') }}
          </v-alert>
        </v-card>

        <v-row dense>
          <v-col cols="12" md="6">
            <v-card class="pa-4 h-100">
              <div class="text-subtitle-2 font-weight-bold mb-3">{{ t('help.compare.states') }}</div>
              <v-table density="compact">
                <thead>
                  <tr>
                    <th style="width: 130px">{{ t('help.compare.state') }}</th>
                    <th>{{ t('help.compare.meaning') }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="s in compareStates" :key="s.code">
                    <td><code class="text-caption">{{ s.code }}</code></td>
                    <td>{{ s.desc }}</td>
                  </tr>
                </tbody>
              </v-table>
            </v-card>
          </v-col>
          <v-col cols="12" md="6">
            <v-card class="pa-4 h-100">
              <div class="text-subtitle-2 font-weight-bold mb-3">{{ t('help.compare.events') }}</div>
              <v-table density="compact">
                <thead>
                  <tr>
                    <th style="width: 170px">{{ t('help.compare.event') }}</th>
                    <th>{{ t('help.compare.trigger') }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="e in compareEvents" :key="e.code">
                    <td><code class="text-caption">{{ e.code }}</code></td>
                    <td>{{ e.desc }}</td>
                  </tr>
                </tbody>
              </v-table>
            </v-card>
          </v-col>
        </v-row>

        <v-card class="pa-4 mt-4">
          <div class="text-subtitle-2 font-weight-bold mb-2">{{ t('help.compare.templateVars') }}</div>
          <p class="text-body-2 mb-2">{{ t('help.compare.templateVarsDesc') }}</p>
          <v-btn size="small" color="primary" variant="tonal" prepend-icon="mdi-bell-ring-outline" @click="activeTab = 'channels'">
            {{ t('help.compare.viewTemplates') }}
          </v-btn>
        </v-card>
      </v-window-item>

      <!-- ================= 版本清洗 ================= -->
      <v-window-item value="filters">
        <v-card class="pa-4 mb-4">
          <div class="text-subtitle-1 font-weight-bold mb-2">{{ t('help.filters.orderTitle') }}</div>
          <p class="text-body-2 mb-0">{{ t('help.filters.orderDesc') }}</p>
        </v-card>

        <v-card class="pa-4">
          <v-table density="comfortable">
            <thead>
              <tr>
                <th style="width: 210px">{{ t('help.filters.field') }}</th>
                <th>{{ t('help.filters.effect') }}</th>
                <th style="width: 300px">{{ t('help.filters.example') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="d in filterDocs" :key="d.field">
                <td>
                  <code class="text-caption">{{ d.field }}</code>
                  <div class="text-caption text-medium-emphasis">{{ d.label }}</div>
                </td>
                <td>{{ d.desc }}</td>
                <td class="text-caption font-mono">{{ d.example }}</td>
              </tr>
            </tbody>
          </v-table>
          <v-alert type="info" variant="tonal" density="compact" class="mt-3">
            {{ t('help.filters.tip') }}
          </v-alert>
        </v-card>
      </v-window-item>

      <!-- ================= nvchecker 迁移 ================= -->
      <v-window-item value="nvchecker">
        <v-card class="pa-4 mb-4">
          <div class="text-subtitle-1 font-weight-bold mb-2">{{ t('help.nvchecker.title') }}</div>
          <p class="text-body-2 mb-0">{{ t('help.nvchecker.desc') }}</p>
          <v-btn class="mt-3" size="small" color="primary" variant="tonal" prepend-icon="mdi-import" to="/import">
            {{ t('help.nvchecker.openImport') }}
          </v-btn>
        </v-card>

        <v-card class="pa-4 mb-4">
          <div class="text-subtitle-2 font-weight-bold mb-3">{{ t('help.nvchecker.mapping') }}</div>
          <v-table density="compact">
            <thead>
              <tr>
                <th style="width: 320px">nvchecker source</th>
                <th>RelayWatch</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="m in nvcheckerMappings" :key="m.from">
                <td><code class="text-caption">{{ m.from }}</code></td>
                <td><code class="text-caption">{{ m.to }}</code></td>
              </tr>
            </tbody>
          </v-table>
        </v-card>

        <v-card class="pa-4">
          <div class="text-subtitle-2 font-weight-bold mb-3">{{ t('help.nvchecker.behavior') }}</div>
          <ul class="text-body-2 ps-6 mb-0">
            <li class="mb-2">{{ t('help.nvchecker.behavior1') }}</li>
            <li class="mb-2">{{ t('help.nvchecker.behavior2') }}</li>
            <li>{{ t('help.nvchecker.behavior3') }}</li>
          </ul>
        </v-card>
      </v-window-item>

      <!-- ================= API 与自动化 ================= -->
      <v-window-item value="api">
        <v-card class="pa-4 mb-4">
          <div class="text-subtitle-1 font-weight-bold mb-2">{{ t('help.api.title') }}</div>
          <p class="text-body-2 mb-0">{{ t('help.api.desc') }}</p>
        </v-card>

        <v-card class="pa-4 mb-4">
          <div class="text-subtitle-2 font-weight-bold mb-2">{{ t('help.api.authTitle') }}</div>
          <p class="text-body-2 mb-3">{{ t('help.api.authDesc') }}</p>
          <div class="pa-3 rounded border font-mono text-body-2 mb-3 d-flex align-center justify-space-between" style="background-color: rgba(var(--v-theme-on-surface), 0.05);">
            <code>Authorization: Bearer &lt;your-token&gt;</code>
            <v-btn
              icon="mdi-content-copy"
              size="small"
              variant="text"
              color="primary"
              @click="copyText('Authorization: Bearer <your-token>', 'Header')"
            />
          </div>
          <div class="d-flex align-center justify-space-between text-body-2">
            <span class="text-medium-emphasis">{{ t('help.api.createTokenTip') }}</span>
            <v-btn size="small" color="primary" variant="tonal" prepend-icon="mdi-key" to="/profile">
              {{ t('help.api.openProfileTokens') }}
            </v-btn>
          </div>
        </v-card>

        <v-card class="pa-4 mb-4">
          <div class="text-subtitle-2 font-weight-bold mb-3">{{ t('help.api.endpointsTitle') }}</div>
          <div v-for="ep in apiExamples" :key="ep.path + ep.method" class="mb-4 pb-3 border-b">
            <div class="d-flex align-center ga-2 mb-1">
              <v-chip size="x-small" :color="ep.method === 'POST' ? 'success' : 'primary'" label class="font-weight-bold">
                {{ ep.method }}
              </v-chip>
              <code class="text-subtitle-2">{{ ep.path }}</code>
              <span class="text-body-2 text-medium-emphasis ms-2">— {{ ep.title }}</span>
            </div>
            <div class="pa-3 rounded border font-mono text-caption mt-2 d-flex align-center justify-space-between" style="background-color: rgba(var(--v-theme-on-surface), 0.05); overflow-x: auto;">
              <code>{{ ep.cmd }}</code>
              <v-btn
                icon="mdi-content-copy"
                size="x-small"
                variant="text"
                color="primary"
                class="ms-2"
                @click="copyText(ep.cmd, ep.method + ' ' + ep.path)"
              />
            </div>
          </div>
        </v-card>

        <v-card class="pa-4">
          <div class="text-subtitle-2 font-weight-bold mb-2">{{ t('help.api.automationTitle') }}</div>
          <p class="text-body-2 mb-3">{{ t('help.api.automationDesc') }}</p>
          <div class="pa-3 rounded border font-mono text-caption d-flex align-center justify-space-between" style="background-color: rgba(var(--v-theme-on-surface), 0.05); overflow-x: auto;">
            <code>0 */2 * * * curl -s -X POST -H "Authorization: Bearer &lt;your-token&gt;" "{{ apiOrigin }}/api/v1/monitors/1/check" &gt; /dev/null</code>
            <v-btn
              icon="mdi-content-copy"
              size="x-small"
              variant="text"
              color="primary"
              class="ms-2"
              @click="copyText(crontabExample, 'Crontab')"
            />
          </div>
        </v-card>
      </v-window-item>
    </v-window>

    <v-snackbar v-model="snackbar">{{ snackbarText }}</v-snackbar>
  </div>
</template>

<style scoped>
.font-mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}
</style>
