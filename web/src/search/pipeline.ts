import {useQuery} from '@tanstack/react-query'
import type {components} from '../api.generated'
import {request} from '../request'

const disabled: components['schemas']['PipelineStatus'] = {embed:'disabled', classify:'disabled', semantic:'disabled', watch:'disabled', blurb:'disabled', summary:'disabled', edition:'disabled', ask:'disabled', compile_rule:'disabled'}

export function usePipeline() {
  const query = useQuery({queryKey:['pipeline'], queryFn:({signal}) => request<components['schemas']['PipelineStatus']>('/v1/pipeline',{signal}), staleTime:30_000})
  return {...query,status:query.data ?? disabled}
}
