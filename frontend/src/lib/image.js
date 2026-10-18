// Картинка для загрузки (фото артиста, обложка): квадрат по центру,
// не больше maxSize px, JPEG до 1 МБ — столько пропускает сервер. Фото с
// телефона по 5–10 МБ так становятся ~200–400 КБ без заметной потери.

const MAX_BYTES = 1024 * 1024;

export async function toSquareJpeg(file, maxSize = 1000) {
  const bitmap = await createImageBitmap(file);
  const side = Math.min(bitmap.width, bitmap.height);
  const size = Math.min(maxSize, side);
  const canvas = document.createElement('canvas');
  canvas.width = size;
  canvas.height = size;
  const ctx = canvas.getContext('2d');
  ctx.drawImage(bitmap, (bitmap.width - side) / 2, (bitmap.height - side) / 2, side, side, 0, 0, size, size);
  bitmap.close?.();

  for (const quality of [0.9, 0.8, 0.7, 0.6]) {
    const blob = await new Promise((resolve) => canvas.toBlob(resolve, 'image/jpeg', quality));
    if (blob && blob.size <= MAX_BYTES) return blob;
  }
  throw new Error('image too large');
}
