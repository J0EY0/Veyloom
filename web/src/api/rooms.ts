import { useQuery } from '@tanstack/react-query'
import { api } from './client'
import type { RoomResponse } from './types'

export const roomKeys = {
  one: (roomId: string) => ['rooms', roomId] as const,
}

export function useRoom(roomId: string) {
  return useQuery({
    queryKey: roomKeys.one(roomId),
    queryFn: async () => (await api.get<RoomResponse>(`/rooms/${roomId}`)).room,
    enabled: roomId !== '',
  })
}
