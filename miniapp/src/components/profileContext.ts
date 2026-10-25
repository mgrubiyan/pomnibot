import { createContext, useContext } from 'react';
import type { Profile } from '../types';

export interface ProfileState {
    profile: Profile;
    setProfile: (profile: Profile) => void;
}

export const ProfileContext = createContext<ProfileState | null>(null);

/** The profile ProfileGate loaded, kept fresh as it reloads; null outside the gate. */
export function useProfile(): ProfileState | null {
    return useContext(ProfileContext);
}
