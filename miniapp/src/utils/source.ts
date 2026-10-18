/**
 * The source of a card as the reader sees it. The reference is built from the
 * file name, so «Этапы_кода.pdf, стр. 2» reads «Этапы кода, стр. 2».
 */
export function sourceRefLabel(ref: string): string {
    return ref
        .replace(/\.(pdf|docx?|pptx?|txt|md|jpe?g|png|webp)(?=,|$)/gi, '')
        .replace(/_+/g, ' ')
        .replace(/\s+,/g, ',')
        .replace(/\s{2,}/g, ' ')
        .trim();
}
