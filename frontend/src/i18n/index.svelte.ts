import { en } from './locales/en';
import { es } from './locales/es';
import type { LanguagePreference, LocaleCode, LanguageOption, TranslationParams } from './types';

export * from './types';

export const SUPPORTED_LANGUAGES: LanguageOption[] = [
  { code: 'system', label: 'System default', nativeName: 'System default' },
  { code: 'en', label: 'English', nativeName: 'English' },
  { code: 'es', label: 'Spanish', nativeName: 'Español' }
];

export type TranslationKey = keyof typeof en;

const translations: Record<LocaleCode, Record<TranslationKey, string>> = {
  en,
  es
};

/**
 * Detects the system language.
 * Checks primary subtags (e.g. 'es-MX', 'es-ES' -> 'es') against available translations.
 * Defaults to 'en' if not matched.
 */
export function detectSystemLanguage(): LocaleCode {
  if (typeof navigator !== 'undefined') {
    const candidates = (navigator.languages && navigator.languages.length > 0)
      ? navigator.languages
      : [navigator.language || (navigator as any).userLanguage || ''];

    for (const cand of candidates) {
      if (!cand) continue;
      const primary = cand.toLowerCase().split('-')[0].split('_')[0] as LocaleCode;
      if (primary in translations) {
        return primary;
      }
    }
  }
  return 'en';
}

class I18nManager {
  // Configured user preference: 'system' | 'en' | 'es'
  preference = $state<LanguagePreference>('system');

  // Currently resolved locale being rendered: 'en' | 'es'
  locale = $state<LocaleCode>('en');

  // Detected system locale: 'en' | 'es'
  systemLocale = $state<LocaleCode>('en');

  constructor() {
    this.systemLocale = detectSystemLanguage();
    this.locale = this.systemLocale;

    if (typeof window !== 'undefined') {
      window.addEventListener('languagechange', () => {
        this.systemLocale = detectSystemLanguage();
        if (this.preference === 'system') {
          this.locale = this.systemLocale;
        }
      });
    }
  }

  setLocale(code: string | undefined | null) {
    if (!code || code === 'system') {
      this.preference = 'system';
      this.locale = this.systemLocale;
      return;
    }
    const normalized = code.toLowerCase().trim() as LocaleCode;
    if (normalized in translations) {
      this.preference = normalized;
      this.locale = normalized;
    } else {
      this.preference = 'system';
      this.locale = this.systemLocale;
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
