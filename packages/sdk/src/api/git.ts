import { postJsonRequest } from "../request";
import { type GitCloneRequest, ExecResponseSchema } from "../schemas";
import { execute } from "../transport";

export const gitClone = (request: GitCloneRequest) =>
	execute(postJsonRequest("/api/git/clone", request), ExecResponseSchema);
