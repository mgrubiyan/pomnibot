import { useEffect, useEffectEvent } from 'react';
import { webApp } from './bridge';

/**
 * How many mounted screens currently want the button. Screens hand it
 * over within one commit — the old one lets go before the new one takes
 * it — so hiding waits for that commit to finish and is skipped when
 * someone took the button in the meantime: no blink between screens.
 */
let holders = 0;

/**
 * Shows the back button in the MAX header while the screen is mounted
 * and routes its press to `onBack` — the same place the screen's own
 * back control leads. Screens without the hook leave it hidden.
 */
export function useBackButton(onBack: () => void) {
    const press = useEffectEvent(onBack);

    useEffect(() => {
        const button = webApp()?.BackButton;
        if (!button) {
            return;
        }

        // A stable reference, so offClick removes exactly this handler.
        const handler = () => press();
        button.onClick(handler);
        holders += 1;
        button.show();

        return () => {
            button.offClick(handler);
            holders -= 1;
            queueMicrotask(() => {
                if (holders === 0) {
                    button.hide();
                }
            });
        };
    }, []);
}
