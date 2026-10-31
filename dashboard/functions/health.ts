import { proxyToMonitor, type PagesContext } from '../cf-proxy';

export const onRequest = (context: PagesContext): Promise<Response> => proxyToMonitor(context);
