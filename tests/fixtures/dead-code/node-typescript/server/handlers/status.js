module.exports.get = async () => ({ statusCode: 200 });

module.exports.payload = async () => ({ ok: true });

const localStatusHelper = () => "not a route root";
