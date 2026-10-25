/** The side of the square photo the mini-app uploads, in pixels. */
export const AVATAR_SIDE = 512;

/** The largest square in the middle of a w×h picture. */
export function centerSquare(width: number, height: number): { x: number; y: number; side: number } {
    const side = Math.min(width, height);
    return { x: Math.round((width - side) / 2), y: Math.round((height - side) / 2), side };
}

/** Cuts the middle square out of a photo the user picked, shrinks it to
    AVATAR_SIDE and returns it as base64 JPEG. Drawing it anew leaves the
    file's metadata behind; the server encodes it again anyway. */
export async function avatarFromFile(file: File): Promise<string> {
    const bitmap = await createImageBitmap(file);
    try {
        const { x, y, side } = centerSquare(bitmap.width, bitmap.height);
        const size = Math.min(AVATAR_SIDE, side);
        const canvas = document.createElement('canvas');
        canvas.width = size;
        canvas.height = size;
        const context = canvas.getContext('2d');
        if (!context) {
            throw new Error('canvas unavailable');
        }
        context.drawImage(bitmap, x, y, side, side, 0, 0, size, size);
        const blob = await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, 'image/jpeg', 0.85));
        if (!blob) {
            throw new Error('photo not encoded');
        }
        const bytes = new Uint8Array(await blob.arrayBuffer());
        let binary = '';
        for (let i = 0; i < bytes.length; i += 0x8000) {
            binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
        }
        return btoa(binary);
    } finally {
        bitmap.close();
    }
}
