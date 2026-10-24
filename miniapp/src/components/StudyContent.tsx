import { memo, type ComponentProps, type HTMLAttributes } from 'react';
import Markdown, { type Components, type ExtraProps } from 'react-markdown';
import remarkGfm from 'remark-gfm';
import remarkMath from 'remark-math';
import rehypeKatex from 'rehype-katex';
import 'katex/dist/katex.min.css';
import { cx } from '../utils/cx';
import s from './StudyContent.module.css';

interface StudyContentProps extends HTMLAttributes<HTMLDivElement> {
    text: string;
    /** Options and sorting chips must not contain links or other controls. */
    inControl?: boolean;
}

function MathSpan({ node, inControl, ...props }: ComponentProps<'span'> & ExtraProps & { inControl?: boolean }) {
    const isMath = Array.isArray(node?.properties.className) && node.properties.className.includes('katex');
    if (!isMath) return <span {...props} />;
    return (
        <span className={s.inlineMath} tabIndex={inControl ? undefined : 0}>
            <span {...props} />
        </span>
    );
}

const components: Components = {
    span: MathSpan,
    table: ({ children }) => (
        <div className={s.scroll} tabIndex={0} role="region" aria-label="Таблица">
            <table>{children}</table>
        </div>
    ),
    pre: ({ children }) => <pre tabIndex={0}>{children}</pre>,
    // Generated illustrations arrive in separate, validated SVG fields.
    img: ({ alt }) => <span>{alt}</span>,
    a: ({ children, href }) => href
        ? <a href={href} target="_blank" rel="noopener noreferrer" onClick={(event) => event.stopPropagation()}>{children}</a>
        : <span>{children}</span>,
};

const controlComponents: Components = {
    ...components,
    table: ({ children }) => <div className={s.scroll}><table>{children}</table></div>,
    pre: ({ children }) => <pre>{children}</pre>,
    a: ({ children }) => <span>{children}</span>,
    input: ({ checked }) => <span aria-hidden="true">{checked ? '☑' : '☐'}</span>,
    span: (props) => <MathSpan {...props} inControl />,
};

/** Render source text without accepting raw HTML or trusted LaTeX commands. */
export const StudyContent = memo(function StudyContent({ text, inControl, className, ...props }: StudyContentProps) {
    return (
        <div {...props} className={cx(s.content, className)}>
            <Markdown
                skipHtml
                remarkPlugins={[remarkGfm, remarkMath]}
                rehypePlugins={[[rehypeKatex, { trust: false, strict: 'ignore', maxSize: 10, maxExpand: 1000 }]]}
                components={inControl ? controlComponents : components}
            >
                {text}
            </Markdown>
        </div>
    );
});
