import path from "path";

export const options = {
  openapi: {
    handlers: path.resolve(__dirname, "../../handlers"),
  },
};

export default { options };
