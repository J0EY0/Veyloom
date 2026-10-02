import { describe, expect, it } from 'vitest'
import { distinctSources } from './sources'

describe('distinctSources', () => {
  it('names each place once: a turn in a topic stands for the topic', () => {
    const topic = { resource: 'veyloom://topic/3', thread_id: 't3', topic_number: 3 }
    const turn = { resource: 'veyloom://turn/9', thread_id: 't3', topic_number: 3, turn_id: 'x9' }
    const again = { resource: 'veyloom://turn/12', thread_id: 't3', topic_number: 3, turn_id: 'x12' }
    const other = { resource: 'veyloom://topic/5', thread_id: 't5', topic_number: 5 }
    const file = { resource: 'veyloom://message/m1', thread_id: 't3', topic_number: 3, message_id: 'm1', title: 'plan.png' }
    const web = { resource: 'https://example.com', title: 'Docs' }
    expect(distinctSources([topic, turn, again, other, file, web])).toEqual([turn, other, file, web])
  })
})
