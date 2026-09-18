import { apiBase } from './base'
import { api } from './client'

interface AvatarResponse {
  avatar: string
}

// Where an avatar is served from. A name always means the same picture.
export function avatarUrl(name: string): string {
  return `${apiBase}/avatars/${name}`
}

// uploadAvatar sends a picture already squared and shrunk, and answers
// with the name an agent's avatar takes.
export async function uploadAvatar(image: Blob): Promise<string> {
  return (await api.send<AvatarResponse>('/avatars', image)).avatar
}
