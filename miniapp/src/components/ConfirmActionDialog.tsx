import { useId, useLayoutEffect, useRef } from 'react';
import { Button, Flex, Typography } from '@maxhub/max-ui';
import s from './Dialog.module.css';

export interface ConfirmActionDialogProps {
    title: string;
    description?: string;
    confirmLabel?: string;
    cancelLabel?: string;
    isDestructive?: boolean;
    onConfirm: () => void;
    onCancel: () => void;
}

export function ConfirmActionDialog({
    title,
    description,
    confirmLabel = 'Подтвердить',
    cancelLabel = 'Отмена',
    isDestructive = false,
    onConfirm,
    onCancel,
}: ConfirmActionDialogProps) {
    const dialogRef = useRef<HTMLDialogElement>(null);
    const defaultBtnRef = useRef<HTMLButtonElement>(null);
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
        dialog.showModal();
        defaultBtnRef.current?.focus();

        return () => {
            dialog.close();
            document.body.style.overflow = bodyOverflow;
            document.documentElement.style.overflow = rootOverflow;
        };
    }, []);

    const dismiss = (action: () => void) => {
        if (!dialogRef.current?.open) return;
        dialogRef.current.close();
        action();
    };

    return (
        <dialog
            ref={dialogRef}
            className={s.dialog}
            aria-labelledby={titleId}
            aria-describedby={description ? descriptionId : undefined}
            onCancel={(event) => {
                event.preventDefault();
                dismiss(onCancel);
            }}
            onPointerDown={(event) => {
                const rect = event.currentTarget.getBoundingClientRect();
                backdropPointerDown.current =
                    event.target === event.currentTarget &&
                    (event.clientX < rect.left ||
                        event.clientX > rect.right ||
                        event.clientY < rect.top ||
                        event.clientY > rect.bottom);
            }}
            onClick={(event) => {
                if (event.target === event.currentTarget && backdropPointerDown.current) {
                    dismiss(onCancel);
                }
            }}
        >
            <Flex direction="column" align="stretch" gap={20}>
                <Flex direction="column" align="stretch" gap={8}>
                    <Typography.Text variant="subheader" asChild>
                        <h2 id={titleId} className={s.title}>
                            {title}
                        </h2>
                    </Typography.Text>
                    {description ? (
                        <Typography.Text id={descriptionId} variant="body" color="secondary">
                            {description}
                        </Typography.Text>
                    ) : null}
                </Flex>
                <Flex direction="column" align="stretch" gap={8}>
                    <Button
                        type="button"
                        size="medium"
                        variant={isDestructive ? 'destructive' : 'primary'}
                        stretched
                        onClick={() => dismiss(onConfirm)}
                    >
                        {confirmLabel}
                    </Button>
                    <Button
                        ref={defaultBtnRef}
                        type="button"
                        size="medium"
                        variant="secondary"
                        stretched
                        onClick={() => dismiss(onCancel)}
                    >
                        {cancelLabel}
                    </Button>
                </Flex>
            </Flex>
        </dialog>
    );
}
