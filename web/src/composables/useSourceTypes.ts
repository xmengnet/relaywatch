import { ref } from 'vue'
import { getSourceTypes } from '@/api/monitors'
import type { SourceMeta } from '@/api/types'

const types = ref<SourceMeta[]>([])
let loaded = false

export function useSourceTypes() {
  async function ensure() {
    if (loaded) return
    loaded = true
    try {
      types.value = (await getSourceTypes()).items
    } catch {
      loaded = false
    }
  }

  function meta(sourceType: string): SourceMeta | undefined {
    return types.value.find(t => t.type === sourceType)
  }

  return { types, ensure, meta }
}
