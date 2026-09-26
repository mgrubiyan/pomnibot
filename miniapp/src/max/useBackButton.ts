import { useEffect, useEffectEvent } from 'react';
import { webApp } from './bridge';

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
        button.show();

        return () => {
            button.offClick(handler);
            button.hide();
        };
    }, []);
}
