import React from 'react';

interface PanelProps {
  title?: React.ReactNode;
  /** Explains what question this panel answers. Kept short and always visible. */
  hint?: React.ReactNode;
  actions?: React.ReactNode;
  children: React.ReactNode;
  /** Panels that hold the primary evidence of a surface can be emphasised. */
  emphasis?: boolean;
  className?: string;
  /** Removes interior padding for panels whose child manages its own gutters (tables). */
  flush?: boolean;
}

export const Panel: React.FC<PanelProps> = ({
  title,
  hint,
  actions,
  children,
  emphasis = false,
  className = '',
  flush = false,
}) => (
  <section className={`pl-panel ${emphasis ? 'is-emphasis' : ''} ${className}`}>
    {(title || actions) && (
      <header className="pl-panel__head">
        <div className="pl-panel__titles">
          {title && <h2 className="pl-panel__title">{title}</h2>}
          {hint && <p className="pl-panel__hint">{hint}</p>}
        </div>
        {actions && <div className="pl-panel__actions">{actions}</div>}
      </header>
    )}
    <div className={flush ? 'pl-panel__body is-flush' : 'pl-panel__body'}>{children}</div>
  </section>
);

/** Lightweight labelled divider used to group fields inside a panel. */
export const FieldGroup: React.FC<{ label: string; children: React.ReactNode }> = ({ label, children }) => (
  <div className="pl-field-group">
    <div className="pl-field-group__label">{label}</div>
    <div className="pl-field-group__body">{children}</div>
  </div>
);

/** A single label/value row. Values use tabular numerals so columns of numbers align. */
export const Field: React.FC<{
  label: string;
  children: React.ReactNode;
  mono?: boolean;
  title?: string;
}> = ({ label, children, mono = true, title }) => (
  <div className="pl-field" title={title}>
    <dt className="pl-field__label">{label}</dt>
    <dd className={mono ? 'pl-field__value is-mono' : 'pl-field__value'}>{children}</dd>
  </div>
);
