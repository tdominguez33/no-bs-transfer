import { en } from './locales/en';
import { es } from './locales/es';
import type { LanguageCode, LanguageOption, TranslationParams } from './types';

export * from './types';

export const SUPPORTED_LANGUAGES: LanguageOption[] = [
  { code: 'en', label: 'English', nativeName: 'English' },
  { code: 'es', label: 'Spanish', nativeName: 'Español' }
];

export type TranslationKey = keyof typeof en;

const translations: Record<LanguageCode, Record<TranslationKey, string>> = {
  en,
  es
};

class I18nManager {
  locale = $state<LanguageCode>('en');

  setLocale(code: string | undefined | null) {
    if (!code) return;
    const normalized = code.toLowerCase().trim() as LanguageCode;
    if (normalized in translations) {
      this.locale = normalized;
    }
  }

  t(key: TranslationKey, params?: TranslationParams): string {
    const dict = translations[this.locale] || translations.en;
    let text = dict[key] ?? translations.en[key] ?? key;

    if (params) {
      for (const [k, v] of Object.entries(params)) {
        text = text.replaceAll(`{${k}}`, String(v));
      }
    }

    return text;
  }
}

export const i18n = new I18nManager();
export const t = (key: TranslationKey, params?: TranslationParams): string => i18n.t(key, params);
