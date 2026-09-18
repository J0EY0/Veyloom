import { useQuery } from '@tanstack/react-query'
import { api } from './client'
import type { TranscriptLine } from './types'

export const transcriptKeys = {
  one: (turnId: string) => ['turns', turnId, 'transcript'] as const,
}

// parseTranscript reads NDJSON, skipping a line that is not valid JSON
// (a file cut off mid-write ends that way).
export function parseTranscript(text: string): TranscriptLine[] {
  const lines: TranscriptLine[] = []
  for (const raw of text.split('\n')) {
    if (raw.trim() === '') continue
    try {
      lines.push(JSON.parse(raw) as TranscriptLine)
    } catch {
      // Ignore a torn line.
    }
  }
  return lines
}

// A finished turn's transcript never changes, so it is fetched once. A
// running turn's file is still buffered on the server, so callers pass
// enabled=false for those and use the live events instead.
export function useTranscript(turnId: string, enabled = true) {
  return useQuery({
    queryKey: transcriptKeys.one(turnId),
    queryFn: async () => parseTranscript(await api.text(`/turns/${turnId}/transcript`)),
    enabled: enabled && turnId !== '',
    staleTime: Infinity,
  })
}
