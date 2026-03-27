function getErrorMessage(error: unknown) {
  if (!(error instanceof Error) || !error.message) {
    return "";
  }

  return error.message;
}

export function isInfrastructureConnectionError(error: unknown) {
  const message = getErrorMessage(error);
  if (!message) {
    return false;
  }

  return [
    "Connection timeout",
    "connect ECONNREFUSED",
    "connect ETIMEDOUT",
    "getaddrinfo ENOTFOUND",
    "Failed to connect",
    "Failed query:",
    "database",
    "relation",
    "redis",
  ].some((fragment) => message.includes(fragment));
}

export function toPlayerFacingGameError(error: unknown, fallback: string) {
  const message = getErrorMessage(error);
  if (!message) {
    return fallback;
  }

  if (isInfrastructureConnectionError(error)) {
    return "这会儿还没法开始，稍后再试一次吧。";
  }

  if (message.includes("游戏不存在")) {
    return "这局已经失效了，重新开一局吧。";
  }

  if (message.includes("只能查看")) {
    return "这是观战链接，只能看看进度，不能代替落猜。";
  }

  if (message.includes("当前题库中找不到对应角色")) {
    return "这位马娘暂时不在当前题库里。";
  }

  if (message.includes("question_bank_ready.json 为空")) {
    return "题库还在整理中，稍后再来试试。";
  }

  return message;
}
