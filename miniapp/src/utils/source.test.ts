import { describe, expect, test } from 'bun:test';
import { sourceRefLabel } from './source';

describe('sourceRefLabel', () => {
    test('turns underscores of a file name into spaces', () => {
        expect(sourceRefLabel('Этапы обработки кода программы на C++_ пространства имён и локали, стр. 8')).toBe(
            'Этапы обработки кода программы на C++ пространства имён и локали, стр. 8',
        );
    });

    test('drops the file extension before the page', () => {
        expect(sourceRefLabel('Лекция_4.pdf, стр. 5')).toBe('Лекция 4, стр. 5');
        expect(sourceRefLabel('Слайды.PPTX')).toBe('Слайды');
    });

    test('keeps a plain reference as is', () => {
        expect(sourceRefLabel('ОРГ Лекция по разделу 4, стр. 5')).toBe('ОРГ Лекция по разделу 4, стр. 5');
        expect(sourceRefLabel('Версия 1.2, стр. 3')).toBe('Версия 1.2, стр. 3');
    });
});
