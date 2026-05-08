export const post = async (request: { payload: unknown }): Promise<unknown> => {
  return request.payload;
};

const localHandlerHelper = (): string => "not exported";
