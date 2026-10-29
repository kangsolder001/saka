import type { components, paths } from './schema';

export type { components, paths };

export type Schema<T extends keyof components['schemas']> = components['schemas'][T];

export type Profile = components['schemas']['Profile'];
export type AuthUser = components['schemas']['AuthUser'];
export type OrganizationRef = components['schemas']['OrganizationRef'];
export type MemberOrganization = components['schemas']['MemberOrganization'];
export type Organization = components['schemas']['Organization'];
export type Member = components['schemas']['Member'];
export type Invitation = components['schemas']['Invitation'];
export type InvitationInfo = components['schemas']['InvitationInfo'];
export type Plan = components['schemas']['Plan'];
export type Subscription = components['schemas']['Subscription'];
export type SubscriptionStatus = components['schemas']['SubscriptionStatus'];
export type Invoice = components['schemas']['Invoice'];
export type Session = components['schemas']['Session'];
export type Role = components['schemas']['Role'];
export type ErrorCode = components['schemas']['ErrorCode'];
export type CheckoutResponse = components['schemas']['CheckoutResponse'];
export type ChangePlanResponse = components['schemas']['ChangePlanResponse'];
export type TokenResponse = components['schemas']['TokenResponse'];
