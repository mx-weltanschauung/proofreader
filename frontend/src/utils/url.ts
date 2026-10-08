import { API_BASE_URL } from '../services/api';

/**
 * Normalizes a file path to ensure it starts with /
 * @param path - The file path (e.g., "uploads/file.png" or "/uploads/file.png")
 * @returns Normalized path starting with / (e.g., "/uploads/file.png")
 */
export function normalizePath(path: string): string {
  return path.startsWith('/') ? path : '/' + path;
}

/**
 * Gets the full URL for a static file (e.g., preview images)
 * In development (with proxy): returns relative URL like "/uploads/file.png"
 * In production (with API_BASE_URL): returns full URL like "http://api.example.com/uploads/file.png"
 *
 * @param path - The file path from the database (e.g., "/work_1/page_1.png" or "work_1/page_1.png")
 * @returns Full URL to access the file
 */
export function getStaticFileUrl(path: string): string {
  const normalizedPath = normalizePath(path);

  if (API_BASE_URL) {
    // Production mode: use full URL with API base
    return `${API_BASE_URL}${normalizedPath}`;
  } else {
    // Development mode: use relative URL (Vite proxy will handle it)
    return normalizedPath;
  }
}

/**
 * Gets the full URL for an uploaded file preview
 * @param previewPath - The preview_path from the database
 * @returns Full URL to access the preview image
 */
export function getPreviewUrl(previewPath: string | null | undefined): string | null {
  if (!previewPath) return null;
  return getStaticFileUrl(previewPath);
}
