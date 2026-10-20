import { useId, useLayoutEffect, useRef } from 'react';
import { Button, Flex, Typography } from '@maxhub/max-ui';
import s from './ConfirmDiscardDialog.module.css';

export function ConfirmDiscardDialog({ onConfirm, onCancel }: {
    onConfirm: () => void;
    onCancel: () => void;
}) {
    const dialogRef = useRef<HTMLDialogElement>(null);
    const stayRef = useRef<HTMLButtonElement>(null);
    const backdropPointerDown = useRef(false);
    const titleId = useId();
    const descriptionId = useId();

    useLayoutEffect(() => {
        const dialog = dialogRef.current;
        if (!dialog) return;

        const bodyOverflow = document.body.style.overflow;
        const rootOverflow = document.documentElement.style.overflow;
        document.body.style.overflow = 'hidden';
        document.documentElement.style.overflow = 'hidden';
        // The native modal makes the background inert and restores focus on close.
        dialog.showModal();
        stayRef.current?.focus();

        return () => {
            dialog.close();
            document.body.style.overflow = bodyOverflow;
            document.documentElement.style.overflow = rootOverflow;
        };
    }, []);

    const dismiss = (action: () => void) => {
        if (!dialogRef.current?.open) return;
        // Close synchronously so repeated clicks cannot run the action twice.
        dialogRef.current.close();
        action();
    };

    return (
        <dialog
            ref={dialogRef}
            className={s.dialog}
            aria-labelledby={titleId}
            aria-describedby={descriptionId}
            onCancel={(event) => { event.preventDefault(); dismiss(onCancel); }}
            onKeyDown={(event) => {
                if (event.key !== 'Tab') return;
                event.preventDefault();
                const buttons = [...event.currentTarget.querySelectorAll('button')];
                const index = buttons.indexOf(document.activeElement as HTMLButtonElement);
                buttons[(index + (event.shiftKey ? -1 : 1) + buttons.length) % buttons.length]?.focus();
            }}
            onPointerDown={(event) => {
                const rect = event.currentTarget.getBoundingClientRect();
                backdropPointerDown.current = event.target === event.currentTarget && (
                    event.clientX < rect.left || event.clientX > rect.right ||
                    event.clientY < rect.top || event.clientY > rect.bottom
                );
            }}
            onClick={(event) => {
                if (event.target === event.currentTarget && backdropPointerDown.current) dismiss(onCancel);
            }}
        >
            <Flex direction="column" align="stretch" gap={20}>
                <Flex direction="column" align="stretch" gap={8}>
                    <Typography.Text variant="subheader" asChild>
                        <h2 id={titleId} className={s.title}>Выйти без сохранения?</h2>
                    </Typography.Text>
                    <Typography.Text id={descriptionId} variant="body" color="secondary">
                        Несохранённые изменения будут потеряны.
                    </Typography.Text>
                </Flex>
                <Flex direction="column" align="stretch" gap={8}>
                    <Button ref={stayRef} type="button" size="medium" variant="secondary" stretched onClick={() => dismiss(onCancel)}>
                        Остаться
                    </Button>
                    <Button type="button" size="medium" variant="destructive" stretched onClick={() => dismiss(onConfirm)}>
                        Выйти без сохранения
                    </Button>
                </Flex>
            </Flex>
        </dialog>
    );
}
