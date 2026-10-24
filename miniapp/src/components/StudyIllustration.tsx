import { memo } from 'react';
import s from './StudyIllustration.module.css';

/** An image document keeps SVG markup out of the application's DOM. */
export const StudyIllustration = memo(function StudyIllustration({ svg, alt }: { svg?: string; alt: string }) {
    if (!svg?.trim()) return null;
    return (
        <img
            key={svg}
            className={s.image}
            src={`data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`}
            alt={alt}
            decoding="async"
            onError={(event) => { event.currentTarget.hidden = true; }}
        />
    );
});
