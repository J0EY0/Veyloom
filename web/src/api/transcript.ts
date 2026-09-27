import { useQuery } from '@tanstack/react-query'
import { seedLiveTurn } from '@/lib/liveTurns'
import { ApiError, api } from './client'
import type { TranscriptLine, TurnEvent } from './types'

export const transcriptKeys = {
  one: (turnId: string) => ['turns', turnId, 'transcript'] as const,
  soFar: (turnId: string) => ['turns', turnId, 'transcript-so-far'] as const,
}

// eventsOf are the runtime's events among a transcript's lines.
export function eventsOf(lines: TranscriptLine[]): TurnEvent[] {
  return lines.flatMap((line) => (line.kind === 'event' && line.event ? [line.event] : []))
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
// running turn's is read with useTranscriptSoFar instead.
export function useTranscript(turnId: string, enabled = true) {
  return useQuery({
    queryKey: transcriptKeys.one(turnId),
    queryFn: async () => parseTranscript(await api.text(`/turns/${turnId}/transcript`)),
    enabled: enabled && turnId !== '',
    staleTime: Infinity,
  })
}

// useTranscriptSoFar reads a running turn's transcript as far as the hub
// has written it, afresh each time a reader opens, and lays its events
// under those the page heard live (seedLiveTurn): what came before the
// page listened, or while its stream was down, is filled in. A turn still
// getting ready has no transcript yet, which reads as nothing.
export function useTranscriptSoFar(turnId: string, enabled: boolean) {
  return useQuery({
    queryKey: transcriptKeys.soFar(turnId),
    queryFn: async () => {
      let text = ''
      try {
        text = await api.text(`/turns/${turnId}/transcript`)
      } catch (err) {
        if (!(err instanceof ApiError && err.status === 404)) throw err
      }
      const lines = parseTranscript(text)
      seedLiveTurn(turnId, eventsOf(lines))
      return lines
    },
    enabled: enabled && turnId !== '',
    staleTime: 0,
    refetchOnMount: 'always',
  })
}
