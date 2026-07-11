import React from 'react';

interface OrderPanelProps {
  customerId: string;
  skus: string[];
}

export function OrderPanel({ customerId, skus }: OrderPanelProps): JSX.Element {
  const total = skus.length;
  return (
    <section className="order-panel">
      <h2>Orders for {customerId}</h2>
      <ul>
        {skus.map((sku) => (
          <li key={sku}>{sku}</li>
        ))}
      </ul>
      <footer>{total} items</footer>
    </section>
  );
}
