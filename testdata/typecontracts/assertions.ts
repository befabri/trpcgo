// Assignment checks alone accept `any`. Compare types to independent expected
// shapes too, so losing inference fails even when valid client calls still work.
export type Equal<A, B> =
  (<T>() => T extends A ? 1 : 2) extends
  (<T>() => T extends B ? 1 : 2) ? true : false;

export type Assert<T extends true> = T;
