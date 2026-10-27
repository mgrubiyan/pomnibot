import { useId, useLayoutEffect, useRef, useState } from 'react';
import { Button, Flex, Typography } from '@maxhub/max-ui';
import { OneLineTextarea } from './OneLineTextarea';
import s from './Dialog.module.css';

export interface InputDialogProps {
    title: string;
    description?: string;
    initialValue?: string;
    placeholder?: string;
    confirmLabel?: string;
    cancelLabel?: string;
    onConfirm: (val: string) => void;
    onCancel: () => void;
}

export function InputDialog({
    title,
    description,
    initialValue = '',
    placeholder,
    confirmLabel = 'Сохранить',
    cancelLabel = 'Отмена',
    onConfirm,
    onCancel,
}: InputDialogProps) {
    const dialogRef = useRef<HTMLDialogElement>(null);
    const fieldRef = useRef<HTMLTextAreaElement>(null);
    const [value, setValue] = useState(initialValue);
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
        // The caret goes after the name, where a rename usually starts.
        const field = fieldRef.current;
        field?.focus();
        field?.setSelectionRange(field.value.length, field.value.length);

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

    const handleConfirm = () => {
        const trimmed = value.trim();
        if (!trimmed) return;
        dismiss(() => onConfirm(trimmed));
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
                {/* A name is one line, but a long one is read whole: the field
                    wraps and grows instead of scrolling sideways. */}
                <OneLineTextarea
                    ref={fieldRef}
                    rows={1}
                    // Enter saves, so the keyboard says so.
                    enterKeyHint="done"
                    mode="secondary"
                    className={s.field}
                    value={value}
                    placeholder={placeholder}
                    maxLength={120}
                    onValueChange={setValue}
                    onSubmit={handleConfirm}
                />
                <Flex direction="column" align="stretch" gap={8}>
                    <Button
                        type="button"
                        size="medium"
                        variant="primary"
                        stretched
                        disabled={!value.trim()}
                        onClick={handleConfirm}
                    >
                        {confirmLabel}
                    </Button>
                    <Button
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
