export type LanguageCode = 'en' | 'es';

export interface LanguageOption {
  code: LanguageCode;
  label: string;
  nativeName: string;
}

export type TranslationParams = Record<string, string | number>;
