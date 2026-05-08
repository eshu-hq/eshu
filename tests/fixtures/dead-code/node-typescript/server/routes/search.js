const searchController = require("../controllers/search-controller");

const searchRoutes = [
  {
    method: "POST",
    path: "/insert_api_key",
    handler: searchController.insertApiKey,
  },
  {
    method: "GET",
    path: "/api_keys",
    handler: searchController.getKeys,
  },
  {
    method: "PUT",
    path: "/api_keys/{id}",
    options: {
      validate: {},
      handler: searchController.updateKey,
    },
  },
];

module.exports = searchRoutes;

const unusedRouteLikeObject = {
  method: "GET",
  path: "/not-mounted",
  handler: searchController.notMounted,
};
