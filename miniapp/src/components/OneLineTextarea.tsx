import type { TextareaProps } from '@maxhub/max-ui';
import { flattenLines } from '../utils/flattenLines';
import { AutoTextarea } from './AutoTextarea';

interface OneLineTextareaProps extends Omit<TextareaProps, 'value' | 'onChange' | 'onKeyDown'> {
    value: string;
    /** The new text, its line breaks already turned into spaces. */
    onValueChange: (value: string) => void;
    /** Enter, or the action key of a phone keyboard. */
    onSubmit: () => void;
}

/**
 * A one-line text that wraps and grows instead of scrolling sideways, so a
 * long one is read whole. Enter submits it; a pasted line break becomes a
 * space.
 */
export function OneLineTextarea({ onValueChange, onSubmit, ...props }: OneLineTextareaProps) {
    return (
        <AutoTextarea
            {...props}
            onChange={(event) => {
                const field = event.target;
                // Some phone keyboards send their action key as a line
                // break with no Enter keydown.
                if ((event.nativeEvent as InputEvent).inputType === 'insertLineBreak') {
                    onSubmit();
                    return;
                }
                if (!field.value.includes('\n')) {
                    onValueChange(field.value);
                    return;
                }
                // The caret stays where the paste ended, not at the end.
                const caret = flattenLines(field.value.slice(0, field.selectionStart)).length;
                onValueChange(flattenLines(field.value));
                requestAnimationFrame(() => field.setSelectionRange(caret, caret));
            }}
            onKeyDown={(event) => {
                // Enter that picks an IME candidate is not a submit, and a
                // held key repeats.
                if (event.key !== 'Enter' || event.nativeEvent.isComposing) return;
                event.preventDefault();
                if (!event.repeat) onSubmit();
            }}
        />
    );
}
