import { useEffect, useId, useRef, useState, type KeyboardEvent } from 'react';
import { IconButton } from '@maxhub/max-ui';
import { IconShare, IconTrash } from './Icons';
import s from './SetActionsMenu.module.css';

export function SetActionsMenu({ shared, onShare, onRemove }: {
    shared: boolean;
    onShare: () => void;
    onRemove: () => void;
}) {
    const [open, setOpen] = useState(false);
    const root = useRef<HTMLDivElement>(null);
    const trigger = useRef<HTMLButtonElement>(null);
    const menu = useRef<HTMLDivElement>(null);
    const menuId = useId();
    const openAtEnd = useRef(false);

    const close = (restoreFocus = true) => {
        setOpen(false);
        if (restoreFocus) trigger.current?.focus();
    };
    const focusItem = (index: number) => {
        const items = menu.current?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]');
        if (items?.length) items[(index + items.length) % items.length].focus();
    };
    useEffect(() => {
        if (!open) return;
        const items = menu.current?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]');
        if (items?.length) items[openAtEnd.current ? items.length - 1 : 0].focus();
        const onPointerDown = (event: PointerEvent) => {
            if (!root.current?.contains(event.target as Node)) {
                setOpen(false);
                trigger.current?.focus();
            }
        };
        document.addEventListener('pointerdown', onPointerDown);
        return () => document.removeEventListener('pointerdown', onPointerDown);
    }, [open]);

    const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
        if (event.key === 'Escape') {
            event.preventDefault();
            event.stopPropagation();
            close();
        } else if (['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) {
            event.preventDefault();
            const items = Array.from(menu.current?.querySelectorAll('[role="menuitem"]') ?? []);
            const current = items.indexOf(document.activeElement as Element);
            focusItem(event.key === 'Home' ? 0 : event.key === 'End' ? items.length - 1 : current + (event.key === 'ArrowDown' ? 1 : -1));
        } else if (event.key === 'Tab') {
            close(false);
        }
    };

    return (
        <div ref={root} className={s.root} onBlur={(event) => {
            if (!event.currentTarget.contains(event.relatedTarget)) close(false);
        }}>
            <IconButton ref={trigger} size="small" variant="ghost" aria-label="Действия с набором"
                aria-haspopup="menu" aria-expanded={open} aria-controls={open ? menuId : undefined}
                onClick={() => {
                    openAtEnd.current = false;
                    if (open) close();
                    else setOpen(true);
                }} onKeyDown={(event) => {
                    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
                        event.preventDefault();
                        openAtEnd.current = event.key === 'ArrowUp';
                        if (!open) setOpen(true);
                        else focusItem(event.key === 'ArrowDown' ? 0 : -1);
                    }
                }}>
                <svg width="20" height="20" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
                    <circle cx="5" cy="12" r="2" /><circle cx="12" cy="12" r="2" /><circle cx="19" cy="12" r="2" />
                </svg>
            </IconButton>
            {open && <div ref={menu} id={menuId} role="menu" aria-label="Действия с набором" className={s.menu} onKeyDown={onKeyDown}>
                <button type="button" role="menuitem" onClick={() => { close(); onShare(); }}>
                    <IconShare size={20} />Поделиться
                </button>
                <button type="button" role="menuitem" className={s.destructive} onClick={() => { close(); onRemove(); }}>
                    <IconTrash size={20} />{shared ? 'Убрать набор' : 'Удалить набор'}
                </button>
            </div>}
        </div>
    );
}
