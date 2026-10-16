import type { Card } from '../types';
import CardForm, { type CardFormProps } from './CardForm';

type CardEditProps = Omit<CardFormProps, 'card' | 'creationKind'> & { card: Card };
export default function CardEdit(props: CardEditProps) {
    return <CardForm {...props} />;
}
