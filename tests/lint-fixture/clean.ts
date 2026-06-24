import { useState } from "react";

export const cleanHandler = (): number => {
  const [value] = useState<number>(0);
  return value;
};
