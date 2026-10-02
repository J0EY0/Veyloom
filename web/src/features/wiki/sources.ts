import type { WikiSource } from '@/api/types'

// distinctSources are a page's sources, each place once: a topic goes
// when a turn in it is there as well, and so does a second turn in the
// same topic, both saying and opening the same.
export function distinctSources(sources: WikiSource[]): WikiSource[] {
  const topic = (source: WikiSource) => (!source.message_id && source.thread_id && source.topic_number ? source.thread_id : undefined)
  const withTurn = new Set(sources.filter((source) => source.turn_id && topic(source)).map(topic))
  const seen = new Set<string>()
  return sources.filter((source) => {
    const thread = topic(source)
    if (!thread) return true
    if (!source.turn_id && withTurn.has(thread)) return false
    const key = `${source.turn_id ? 'turn' : 'topic'}:${thread}`
    if (seen.has(key)) return false
    seen.add(key)
    return true
  })
}
