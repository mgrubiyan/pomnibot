import { describe, expect, it } from 'bun:test';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { StudyContent } from './StudyContent';
import { StudyIllustration } from './StudyIllustration';

const render = (text: string, inControl = false) => renderToStaticMarkup(createElement(StudyContent, { text, inControl }));

describe('study content', () => {
    it('renders GFM tables, lists, code and inline and display LaTeX', () => {
        const html = render('**Правило** $\\frac{1}{2}$\n\n$$\n\\sum_{n=1}^{3} n = 6\n$$\n\n| x | y |\n|---|---|\n| 1 | 2 |\n\n- Пункт\n\n`x < 2`');
        expect(html).toContain('<strong>Правило</strong>');
        expect(html).toContain('<table>');
        expect(html).toContain('<td>2</td>');
        expect(html).toContain('<li>Пункт</li>');
        expect(html).toContain('<code>x &lt; 2</code>');
        expect(html).toContain('class="katex"');
        expect(html).toContain('class="katex-display"');
        expect(html).toContain('<math');
    });

    it('keeps old plain text readable and tolerates invalid formulas', () => {
        expect(render('Старая карточка\nСо второй строкой')).toContain('Старая карточка\nСо второй строкой');
        expect(render('$\\unknowncommand{$')).toContain('katex-error');
        expect(render('    x = 2')).toContain('<pre');
    });

    it('does not turn untrusted content into HTML, scripts or external images', () => {
        const html = render('<script>alert(1)</script>\n\n<img src="x" onerror="alert(1)"/>\n\n![Подпись](https://example.com/track.png)\n\n[опасно](javascript:alert%281%29)\n\n$\\includegraphics{https://example.com/track.png}$');
        expect(html).not.toContain('<script');
        expect(html).not.toContain('<img');
        expect(html).not.toContain('onerror=');
        expect(html).not.toContain('href="javascript:');
        expect(html).toContain('Подпись');
    });

    it('keeps links and task lists from adding controls inside answer buttons', () => {
        const html = render('[Опция](https://example.com)\n\n- [x] Верно\n\n$\\alpha$\n\n| x |\n|---|\n| y |', true);
        expect(html).toContain('Опция');
        expect(html).not.toContain('<a ');
        expect(html).not.toContain('<input');
        expect(html).not.toContain('tabindex=');
    });
});

describe('study illustrations', () => {
    it('renders SVG as a separate image document and keeps its source intact', () => {
        const svg = '<svg viewBox="0 0 1 1"><text>α &amp; β</text></svg>';
        const html = renderToStaticMarkup(createElement(StudyIllustration, { svg, alt: 'Схема' }));
        expect(html).toContain('<img');
        expect(html).toContain(`data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`);
        expect(html).toContain('alt="Схема"');
        expect(html).not.toContain('<svg');
    });

    it('leaves no empty image for cards without illustrations', () => {
        expect(renderToStaticMarkup(createElement(StudyIllustration, { alt: 'Схема' }))).toBe('');
        expect(renderToStaticMarkup(createElement(StudyIllustration, { svg: ' ', alt: 'Схема' }))).toBe('');
    });
});
