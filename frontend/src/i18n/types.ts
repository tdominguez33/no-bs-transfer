export type LanguagePreference = 'system' | 'en' | 'es';
export type LocaleCode = 'en' | 'es';
export type LanguageCode = LanguagePreference;

export interface LanguageOption {
  code: LanguagePreference;
  label: string;
  nativeName: string;
}

export type TranslationParams = Record<string, string | number>;
