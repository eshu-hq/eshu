// Type declarations for console-contract-types.mjs so homeContracts.test.ts
// can import its renderer from strict TypeScript. Keep in lockstep with the
// exports in console-contract-types.mjs.
export declare const contractsJsonPath: string;
export declare const contractTypesDir: string;
export declare function typeNameForRoute(route: string): string;
export declare function renderContractTypes(contracts: Record<string, unknown>): Record<string, string>;
