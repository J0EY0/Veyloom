// The side of the square an avatar is kept at: sharp at the largest place
// it is shown, on a dense screen.
export const avatarSide = 256

// The pictures an avatar may start from.
export const avatarInputTypes = ['image/png', 'image/jpeg', 'image/webp']

// squareAvatar crops a picture to its centre square and shrinks it to
// avatarSide (a smaller one keeps its size), as WebP where the browser can
// write it and PNG otherwise.
export async function squareAvatar(file: Blob): Promise<Blob> {
  const bitmap = await createImageBitmap(file)
  try {
    const side = Math.min(bitmap.width, bitmap.height)
    const out = Math.min(side, avatarSide)
    const canvas = document.createElement('canvas')
    canvas.width = out
    canvas.height = out
    const context = canvas.getContext('2d')
    if (!context) throw new Error('this browser cannot draw the picture')
    context.imageSmoothingQuality = 'high'
    context.drawImage(bitmap, (bitmap.width - side) / 2, (bitmap.height - side) / 2, side, side, 0, 0, out, out)
    const webp = await encode(canvas, 'image/webp')
    return webp.type === 'image/webp' ? webp : encode(canvas, 'image/png')
  } finally {
    bitmap.close()
  }
}

// A browser that cannot write WebP hands back a PNG instead.
function encode(canvas: HTMLCanvasElement, type: string): Promise<Blob> {
  return new Promise((resolve, reject) => {
    canvas.toBlob((blob) => (blob ? resolve(blob) : reject(new Error('could not encode the picture'))), type, 0.9)
  })
}
